// cmd/profilesvc starts the internal ProfileService gRPC server, with
// reflection enabled so it can be introspected with grpcurl.
package main

import (
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"gosocial/internal/env"
	"gosocial/internal/profilesvc"
	"gosocial/pb"
)

func main() {
	addr := env.Getenv("PROFILESVC_ADDR", ":9091")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	svc := profilesvc.New()
	s := grpc.NewServer()
	pb.RegisterProfileServiceServer(s, svc)
	reflection.Register(s)

	fmt.Printf("profilesvc gRPC server listening on %s\n", addr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
