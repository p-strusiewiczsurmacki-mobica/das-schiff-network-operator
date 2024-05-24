package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/go-logr/zapr"
	netnsadapter "github.com/telekom/das-schiff-network-operator/pkg/adapters/netns"
	vrfigbpadapter "github.com/telekom/das-schiff-network-operator/pkg/adapters/vrf_igbp"
	"github.com/telekom/das-schiff-network-operator/pkg/agent"
	agentpb "github.com/telekom/das-schiff-network-operator/pkg/agent/pb"
	"github.com/telekom/das-schiff-network-operator/pkg/anycast"
	"github.com/telekom/das-schiff-network-operator/pkg/nl"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func main() {
	var agentType string
	var configFile string
	var port int
	flag.StringVar(&configFile, "config", "",
		"The controller will load its initial configuration from this file. "+
			"Omit this flag to use the default configuration values. "+
			"Command-line flags override configuration from this file.")
	flag.StringVar(&agentType, "agent", "legacy", "Use selected agent type (default: legacy).")
	flag.IntVar(&port, "port", agent.DefaultPort, fmt.Sprintf("gRPC listening port. (default: %d)", agent.DefaultPort))

	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	zc.DisableStacktrace = true
	z, _ := zc.Build()
	log := zapr.NewLogger(z)
	log = log.WithName("agent")

	log.Info("agent's port", "port", port)

	var err error

	anycastTracker := anycast.NewTracker(&nl.Toolkit{})

	var adapter agent.Adapter
	switch agentType {
	case "netconf":
		adapter, err = netnsadapter.New()
	default:
		adapter, err = vrfigbpadapter.New(anycastTracker, log)
	}

	log.Info("created adapter", "type", agentType)

	if err != nil {
		log.Error(err, "error creating adapter")
		os.Exit(1)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Error(err, "error on listening start")
		os.Exit(1)
	}

	grpcServer := grpc.NewServer([]grpc.ServerOption{}...)
	srv := agent.NewServer(adapter, &log)
	agentpb.RegisterAgentServer(grpcServer, srv)

	log.Info("created server, start listening...")

	if err := grpcServer.Serve(lis); err != nil {
		log.Error(err, "grpc server error")
		os.Exit(1)
	}
}
