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
package buildin

import (
	"crypto/tls"
	"github.com/apache/incubator-servicecomb-service-center/pkg/tlsutil"
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
	"github.com/apache/incubator-servicecomb-service-center/server/core"
	"github.com/apache/incubator-servicecomb-service-center/server/plugin"
	"github.com/astaxie/beego"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
)

var (
	clientTLSConfig *tls.Config
	serverTLSConfig *tls.Config
	mux             sync.Mutex
)

func GetSSLPath(path string) string {
	env := os.Getenv("SSL_ROOT")
	if len(env) == 0 {
		wd, _ := os.Getwd()
		return filepath.Join(wd, "etc", "ssl", path)
	}
	return os.ExpandEnv(filepath.Join("$SSL_ROOT", path))
}

func GetPassphase() (pass string, decrypt string) {
	passphase, err := ioutil.ReadFile(GetSSLPath("cert_pwd"))
	if err != nil {
		util.Logger().Warn("read file cert_pwd failed.", err)
	}

	pass = util.BytesToStringWithNoCopy(passphase)
	if len(pass) > 0 {
		decrypt, err = plugin.Plugins().Cipher().Decrypt(pass)
		if err != nil {
			util.Logger().Warnf(err, "decrypt ssl passphase(%d) failed.", len(pass))
			decrypt = ""
		}
	}
	return pass, decrypt
}

func GetClientTLSConfig() (_ *tls.Config, err error) {
	mux.Lock()
	defer mux.Unlock()
	if clientTLSConfig != nil {
		return clientTLSConfig, nil
	}

	passphase, decrypt := GetPassphase()

	opts := append(tlsutil.DefaultClientTLSOptions(),
		tlsutil.WithVerifyPeer(core.ServerInfo.Config.SslVerifyPeer),
		tlsutil.WithVerifyHostName(false),
		tlsutil.WithVersion(
			tlsutil.ParseSSLProtocol(
				beego.AppConfig.DefaultString("ssl_client_min_version", core.ServerInfo.Config.SslMinVersion)),
			tls.VersionTLS12),
		tlsutil.WithCipherSuits(tlsutil.ParseDefaultSSLCipherSuites(beego.AppConfig.String("ssl_client_ciphers"))),
		tlsutil.WithKeyPass(decrypt),
		tlsutil.WithCA(GetSSLPath("trust.cer")),
		tlsutil.WithCert(GetSSLPath("server.cer")),
		tlsutil.WithKey(GetSSLPath("server_key.pem")),
	)
	clientTLSConfig, err = tlsutil.GetClientTLSConfig(opts...)

	if clientTLSConfig != nil {
		util.Logger().Infof("client ssl configs enabled, verifyclient %t, minv %#x, cipers %d, pphase %d.",
			core.ServerInfo.Config.SslVerifyPeer,
			clientTLSConfig.MinVersion,
			len(clientTLSConfig.CipherSuites),
			len(passphase))
	}
	return clientTLSConfig, err
}

func GetServerTLSConfig() (_ *tls.Config, err error) {
	mux.Lock()
	defer mux.Unlock()
	if serverTLSConfig != nil {
		return serverTLSConfig, nil
	}

	passphase, decrypt := GetPassphase()

	opts := append(tlsutil.DefaultServerTLSOptions(),
		tlsutil.WithVerifyPeer(core.ServerInfo.Config.SslVerifyPeer),
		tlsutil.WithVersion(tlsutil.ParseSSLProtocol(core.ServerInfo.Config.SslMinVersion), tls.VersionTLS12),
		tlsutil.WithCipherSuits(tlsutil.ParseDefaultSSLCipherSuites(core.ServerInfo.Config.SslCiphers)),
		tlsutil.WithKeyPass(decrypt),
		tlsutil.WithCA(GetSSLPath("trust.cer")),
		tlsutil.WithCert(GetSSLPath("server.cer")),
		tlsutil.WithKey(GetSSLPath("server_key.pem")),
	)

	serverTLSConfig, err = tlsutil.GetServerTLSConfig(opts...)

	if serverTLSConfig != nil {
		util.Logger().Infof("server ssl configs enabled, verifyClient %t, minv %#x, ciphers %d, phase %d.",
			core.ServerInfo.Config.SslVerifyPeer,
			serverTLSConfig.MinVersion,
			len(serverTLSConfig.CipherSuites),
			len(passphase))
	}
	return serverTLSConfig, err
}
