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
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
	"github.com/apache/incubator-servicecomb-service-center/server/core"
	"github.com/apache/incubator-servicecomb-service-center/server/core/backend"
	pb "github.com/apache/incubator-servicecomb-service-center/server/core/proto"
	"github.com/apache/incubator-servicecomb-service-center/server/infra/registry"
	"github.com/apache/incubator-servicecomb-service-center/server/mux"
	serviceUtil "github.com/apache/incubator-servicecomb-service-center/server/service/util"
	"golang.org/x/net/context"
	"time"
)

type DependencyEventHandler struct {
	signals *util.UniQueue
}

func (h *DependencyEventHandler) Type() backend.StoreType {
	return backend.DEPENDENCY_QUEUE
}

func (h *DependencyEventHandler) OnEvent(evt backend.KvEvent) {
	action := evt.Type
	if action != pb.EVT_CREATE && action != pb.EVT_UPDATE && action != pb.EVT_INIT {
		return
	}

	h.signals.Put(struct{}{})
}

func (h *DependencyEventHandler) loop() {
	util.Go(func(ctx context.Context) {
		retries := 0
		delay := func() {
			<-time.After(util.GetBackoff().Delay(retries))
			retries++

			h.signals.Put(struct{}{})
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.signals.Chan():
				lock, err := mux.Try(mux.DEP_QUEUE_LOCK)
				if err != nil {
					util.Logger().Errorf(err, "try to lock %s failed", mux.DEP_QUEUE_LOCK)
					delay()
					continue
				}

				if lock == nil {
					retries = 0
					continue
				}

				err = h.Handle()
				lock.Unlock()
				if err != nil {
					util.Logger().Errorf(err, "handle dependency event failed")
					delay()
					continue
				}

				retries = 0
			}
		}
	})
}

type DependencyEventHandlerResource struct {
	dep           *pb.ConsumerDependency
	kv            *backend.KeyValue
	domainProject string
}

func NewDependencyEventHandlerResource(dep *pb.ConsumerDependency, kv *backend.KeyValue, domainProject string) *DependencyEventHandlerResource {
	return &DependencyEventHandlerResource{
		dep,
		kv,
		domainProject,
	}
}

func isAddToLeft(centerNode *util.Node, addRes interface{}) bool {
	res := addRes.(*DependencyEventHandlerResource)
	compareRes := centerNode.Res.(*DependencyEventHandlerResource)
	if res.kv.ModRevision > compareRes.kv.ModRevision {
		return false
	}
	return true
}

func (h *DependencyEventHandler) Handle() error {
	key := core.GetServiceDependencyQueueRootKey("")
	resp, err := backend.Store().DependencyQueue().Search(context.Background(),
		registry.WithStrKey(key),
		registry.WithPrefix())
	if err != nil {
		return err
	}

	// maintain dependency rules.
	l := len(resp.Kvs)
	if l == 0 {
		return nil
	}

	dependencyTree := util.NewTree(isAddToLeft)

	for _, kv := range resp.Kvs {
		r := kv.Value.(*pb.ConsumerDependency)

		_, domainProject := backend.GetInfoFromDependencyQueueKV(kv)
		res := NewDependencyEventHandlerResource(r, kv, domainProject)

		dependencyTree.AddNode(res)
	}

	return dependencyTree.InOrderTraversal(dependencyTree.GetRoot(), h.dependencyRuleHandle)
}

func (h *DependencyEventHandler) dependencyRuleHandle(res interface{}) error {
	ctx := context.Background()
	dependencyEventHandlerRes := res.(*DependencyEventHandlerResource)
	r := dependencyEventHandlerRes.dep
	consumerFlag := util.StringJoin([]string{r.Consumer.AppId, r.Consumer.ServiceName, r.Consumer.Version}, "/")

	domainProject := dependencyEventHandlerRes.domainProject
	consumerInfo := pb.DependenciesToKeys([]*pb.MicroServiceKey{r.Consumer}, domainProject)[0]
	providersInfo := pb.DependenciesToKeys(r.Providers, domainProject)

	var dep serviceUtil.Dependency
	var err error
	dep.DomainProject = domainProject
	dep.Consumer = consumerInfo
	dep.ProvidersRule = providersInfo
	if r.Override {
		err = serviceUtil.CreateDependencyRule(ctx, &dep)
	} else {
		err = serviceUtil.AddDependencyRule(ctx, &dep)
	}

	if err != nil {
		util.Logger().Errorf(err, "modify dependency rule failed, override: %t, consumer %s", r.Override, consumerFlag)
		return fmt.Errorf("override: %t, consumer is %s, %s", r.Override, consumerFlag, err.Error())
	}

	if err = h.removeKV(ctx, dependencyEventHandlerRes.kv); err != nil {
		util.Logger().Errorf(err, "remove dependency rule failed, override: %t, consumer %s", r.Override, consumerFlag)
		return err
	}

	util.Logger().Infof("maintain dependency %v successfully, override: %t", r, r.Override)
	return nil
}

func (h *DependencyEventHandler) removeKV(ctx context.Context, kv *backend.KeyValue) error {
	dResp, err := backend.Registry().TxnWithCmp(ctx, []registry.PluginOp{registry.OpDel(registry.WithKey(kv.Key))},
		[]registry.CompareOp{registry.OpCmp(registry.CmpVer(kv.Key), registry.CMP_EQUAL, kv.Version)},
		nil)
	if err != nil {
		return fmt.Errorf("can not remove the dependency %s request, %s", util.BytesToStringWithNoCopy(kv.Key), err.Error())
	}
	if !dResp.Succeeded {
		util.Logger().Infof("the dependency %s request is changed", util.BytesToStringWithNoCopy(kv.Key))
	}
	return nil
}

func NewDependencyEventHandler() *DependencyEventHandler {
	h := &DependencyEventHandler{
		signals: util.NewUniQueue(),
	}
	h.loop()
	return h
}
