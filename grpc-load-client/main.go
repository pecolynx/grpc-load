package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/montanaflynn/stats"
	pb "github.com/pecolynx/grpc-load/proto/src/proto"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type Result struct {
	StatusCode   int
	ResponseTime time.Duration
}

func main() {

	filedata, err := os.ReadFile("test.wav")
	if err != nil {
		panic(err)
	}
	results := make([]Result, 0, 10*1000)
	resultCh := make(chan Result)

	fmt.Println("")
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
		grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	client := pb.NewHashServiceClient(conn)
	ctx := context.Background()

	go func(ctx context.Context, resultCh <-chan Result) {
		for {
			select {
			case <-ctx.Done():
				return
			case result := <-resultCh:
				results = append(results, result)
				// fmt.Printf("Recv:%v\n", result)
			}
		}
	}(ctx, resultCh)

	fn := func() error {
		// fmt.Println(time.Now().Format(time.RFC3339))
		now := time.Now()

		stream, err := client.HashConcatStream(ctx)
		if err != nil {
			return err
		}

		if err := stream.Send(&pb.HashRequest{
			Data: filedata,
		}); err != nil && err != io.EOF {
			return err
		}

		statusCode := int(codes.OK)
		if _, err := stream.CloseAndRecv(); err != nil {
			if status, ok := status.FromError(err); ok {
				statusCode = int(status.Code())
			} else {
				statusCode = -1
			}
		}
		result := Result{
			ResponseTime: time.Since(now),
			StatusCode:   statusCode,
		}
		resultCh <- result
		return nil
	}

	var wg sync.WaitGroup
	concurrent := 10000
	wg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer wg.Done()
			run(ctx, 4, time.Second*1, fn)
		}()
	}
	wg.Wait()
	time.Sleep(time.Second * 1)

	responseTimeList := make([]float64, 0, 10*1000)
	for _, result := range results {
		responseTimeList = append(responseTimeList, float64(result.ResponseTime.Milliseconds()))
	}

	fmt.Printf("size: %d\n", len(results))
	m, err := stats.Percentile(responseTimeList, 95)
	if err != nil {
		panic(err)
	}
	fmt.Printf("95th percentile: %f[ms]\n", m)
}

func run(ctx context.Context, loop int, interval time.Duration, fn func() error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for i := 0; i < loop; i++ {
		if err := fn(); err != nil {
			fmt.Println(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
