package agent

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"
)

type Server struct {
	UnimplementedAgentServer
}

func (s Server) SetConfiguration(ctx context.Context, nc *NetworkConfiguration) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
