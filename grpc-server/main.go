package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/pecolynx/grpc-load/proto/src/proto"
)

func main() {
	result := run(context.Background())

	gracefulShutdownTime2 := time.Duration(1) * time.Second
	time.Sleep(gracefulShutdownTime2)
	logrus.Info("exited")
	os.Exit(result)
}

type hashServer struct {
	pb.UnimplementedHashServiceServer
}

func NewHashServer() pb.HashServiceServer {
	return &hashServer{}
}

func (s *hashServer) HashConcatStream(stream pb.HashService_HashConcatStreamServer) error {
	value := ""
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.HashResponse{
				Value: value,
			})
		}
		if err != nil {
			return err
		}
		r := sha256.Sum256(req.Data)
		value += hex.EncodeToString(r[:])
	}
}

func run(ctx context.Context) int {
	var eg *errgroup.Group
	eg, ctx = errgroup.WithContext(ctx)

	eg.Go(func() error {
		return grpcServer(ctx)
	})
	eg.Go(func() error {
		return SignalWatchProcess(ctx)
	})
	eg.Go(func() error {
		<-ctx.Done()
		return ctx.Err()
	})

	if err := eg.Wait(); err != nil {
		logrus.Error(err)
		return 1
	}
	return 0
}

func grpcServer(ctx context.Context) error {
	lis, err := net.Listen("tcp", ":"+strconv.Itoa(50051))
	if err != nil {
		logrus.Fatalf("failed to Listen: %v", err)
		return err
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			otelgrpc.UnaryServerInterceptor(),
			grpc_prometheus.UnaryServerInterceptor,
			grpc_recovery.UnaryServerInterceptor(),
		)),
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			otelgrpc.StreamServerInterceptor(),
			grpc_prometheus.StreamServerInterceptor,
			grpc_recovery.StreamServerInterceptor(),
		)),
	)
	reflection.Register(grpcServer)

	userServer := NewHashServer()
	pb.RegisterHashServiceServer(grpcServer, userServer)

	logrus.Printf("grpc server listening at %v", lis.Addr())

	errCh := make(chan error)
	go func() {
		defer close(errCh)
		if err := grpcServer.Serve(lis); err != nil {
			logrus.Fatalf("failed to Serve: %v", err)
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

func SignalWatchProcess(ctx context.Context) error {
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		signal.Reset()
		return nil
	case sig := <-sigs:
		return fmt.Errorf("signal received: %v", sig.String())
	}
}
