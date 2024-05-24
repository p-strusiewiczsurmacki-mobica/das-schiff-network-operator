package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"

	"github.com/telekom/das-schiff-network-operator/pkg/agent"

	ctrl "sigs.k8s.io/controller-runtime"
)

const defaultPort = 50042

func main() {
	log := ctrl.Log.WithName("agent")

	var port int
	flag.IntVar(&port, "update-limit", defaultPort,
		"Defines how many nodes can be configured at once (default: 1).")

	log.Info("agent's port", "port", port)

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		log.Error(err, "error on listening start")
		os.Exit(1)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	agent.RegisterAgentServer(grpcServer, agent.Server{})
	if err := grpcServer.Serve(lis); err != nil {
		log.Error(err, "grpc server error")
		os.Exit(1)
	}
}
