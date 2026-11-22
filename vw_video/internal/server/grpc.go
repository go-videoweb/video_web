package server

import (
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"util/helper"
	commentv1 "vw_video/api/v1/comment"
	interactv1 "vw_video/api/v1/interact"
	infov1 "vw_video/api/v1/videoinfo"
	"vw_video/internal/conf"
	"vw_video/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/grpc"
)

// NewGRPCServer new a gRPC server.
func NewGRPCServer(
	c *conf.Server,
	videoInfoService *service.VideoInfoService,
	videoInteractService *service.InteractService,
	videoCommentService *service.VideoCommentService,
	logger log.Logger) *grpc.Server {
	var opts = []grpc.ServerOption{
		grpc.Middleware(
			helper.BusinessErrorMiddleware(),
			recovery.Recovery(),
			tracing.Server(),
			logging.Server(logger),
		),
		grpc.StreamMiddleware(helper.BusinessErrorMiddleware()),
	}
	if c.Grpc.Network != "" {
		opts = append(opts, grpc.Network(c.Grpc.Network))
	}
	if c.Grpc.Addr != "" {
		opts = append(opts, grpc.Address(c.Grpc.Addr))
	}
	if c.Grpc.Timeout != nil {
		opts = append(opts, grpc.Timeout(c.Grpc.Timeout.AsDuration()))
	}
	srv := grpc.NewServer(opts...)

	infov1.RegisterVideoInfoServer(srv, videoInfoService)
	interactv1.RegisterVideoInteractServer(srv, videoInteractService)
	commentv1.RegisterVideoCommentServer(srv, videoCommentService)
	return srv
}
