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
package notification

import (
	"errors"
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
)

type Subscriber interface {
	Id() string
	Subject() string
	Group() string
	Type() NotifyType
	Service() *NotifyService
	SetService(*NotifyService)

	Err() error
	SetError(err error)

	Close()
	OnAccept()
	// The event bus will callback this function, so it must be non-blocked.
	OnMessage(job NotifyJob)
}

type BaseSubscriber struct {
	id      string
	subject string
	group   string
	nType   NotifyType
	service *NotifyService
	err     error
}

func (s *BaseSubscriber) Id() string                    { return s.id }
func (s *BaseSubscriber) Subject() string               { return s.subject }
func (s *BaseSubscriber) Group() string                 { return s.group }
func (s *BaseSubscriber) Type() NotifyType              { return s.nType }
func (s *BaseSubscriber) Service() *NotifyService       { return s.service }
func (s *BaseSubscriber) SetService(svc *NotifyService) { s.service = svc }
func (s *BaseSubscriber) Err() error                    { return s.err }
func (s *BaseSubscriber) SetError(err error)            { s.err = err }
func (s *BaseSubscriber) Close()                        {}
func (s *BaseSubscriber) OnAccept()                     {}
func (s *BaseSubscriber) OnMessage(job NotifyJob) {
	s.SetError(errors.New("do not call base notifier OnMessage method"))
}

func NewSubscriber(nType NotifyType, subject, group string) *BaseSubscriber {
	return &BaseSubscriber{
		id:      util.GenerateUuid(),
		group:   group,
		subject: subject,
		nType:   nType,
	}
}
