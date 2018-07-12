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
package event

import (
	"fmt"
	"github.com/apache/incubator-servicecomb-service-center/pkg/async"
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
	"github.com/apache/incubator-servicecomb-service-center/server/core/backend"
	pb "github.com/apache/incubator-servicecomb-service-center/server/core/proto"
	nf "github.com/apache/incubator-servicecomb-service-center/server/service/notification"
	serviceUtil "github.com/apache/incubator-servicecomb-service-center/server/service/util"
	"golang.org/x/net/context"
)

type RulesChangedTask struct {
	key string
	err error

	DomainProject string
	ProviderId    string
	Rev           int64
}

func (apt *RulesChangedTask) Key() string {
	return apt.key
}

func (apt *RulesChangedTask) Do(ctx context.Context) error {
	apt.err = apt.publish(ctx, apt.DomainProject, apt.ProviderId, apt.Rev)
	return apt.err
}

func (apt *RulesChangedTask) Err() error {
	return apt.err
}

func (apt *RulesChangedTask) publish(ctx context.Context, domainProject, providerId string, rev int64) error {
	ctx = util.SetContext(ctx, serviceUtil.CTX_CACHEONLY, "1")

	provider, err := serviceUtil.GetService(ctx, domainProject, providerId)
	if err != nil {
		util.Logger().Errorf(err, "get provider %s service file failed", providerId)
		return err
	}
	if provider == nil {
		util.Logger().Errorf(nil, "provider %s does not exist", providerId)
		return fmt.Errorf("provider %s does not exist", providerId)
	}

	consumerIds, err := serviceUtil.GetConsumerIds(ctx, domainProject, provider)
	if err != nil {
		util.Logger().Errorf(err, "get consumer services by provider %s failed", providerId)
		return err
	}
	providerKey := pb.MicroServiceToKey(domainProject, provider)

	PublishInstanceEvent(domainProject, pb.EVT_EXPIRE, providerKey, nil, rev, consumerIds)
	return nil
}

type RuleEventHandler struct {
}

func (h *RuleEventHandler) Type() backend.StoreType {
	return backend.RULE
}

func (h *RuleEventHandler) OnEvent(evt backend.KvEvent) {
	action := evt.Type
	if action == pb.EVT_INIT {
		return
	}

	providerId, ruleId, domainProject := backend.GetInfoFromRuleKV(evt.KV)
	if nf.GetNotifyService().Closed() {
		util.Logger().Warnf(nil, "caught [%s] service rule event %s/%s, but notify service is closed",
			action, providerId, ruleId)
		return
	}
	util.Logger().Infof("caught [%s] service rule event %s/%s", action, providerId, ruleId)

	async.Service().Add(context.Background(),
		NewRulesChangedAsyncTask(domainProject, providerId, evt.Revision))
}

func NewRuleEventHandler() *RuleEventHandler {
	return &RuleEventHandler{}
}

func NewRulesChangedAsyncTask(domainProject, providerId string, rev int64) *RulesChangedTask {
	return &RulesChangedTask{
		key:           "RulesChangedAsyncTask_" + providerId,
		DomainProject: domainProject,
		ProviderId:    providerId,
		Rev:           rev,
	}
}
