/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package rpc

import (
	"github.com/apache/incubator-servicecomb-service-center/pkg/rpc"
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
	"github.com/apache/incubator-servicecomb-service-center/server/core"
	"github.com/apache/incubator-servicecomb-service-center/server/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
)

type Server struct {
	*grpc.Server
	innerListener net.Listener
}

func (srv *Server) Serve() error {
	return srv.Server.Serve(srv.innerListener)
}

func NewServer(ipAddr string) (_ *Server, err error) {
	var grpcSrv *grpc.Server
	if core.ServerInfo.Config.SslEnabled {
		tlsConfig, err := plugin.Plugins().TLS().ServerConfig()
		if err != nil {
			util.Logger().Error("error to get server tls config", err)
			return nil, err
		}
		creds := credentials.NewTLS(tlsConfig)
		grpcSrv = grpc.NewServer(grpc.Creds(creds))
	} else {
		grpcSrv = grpc.NewServer()
	}

	rpc.RegisterServer(grpcSrv)

	ls, err := net.Listen("tcp", ipAddr)
	if err != nil {
		util.Logger().Error("error to start Grpc API server "+ipAddr, err)
		return
	}

	return &Server{
		Server:        grpcSrv,
		innerListener: ls,
	}, nil
}
