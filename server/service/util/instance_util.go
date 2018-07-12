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
package util

import (
	"encoding/json"
	"fmt"
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
	apt "github.com/apache/incubator-servicecomb-service-center/server/core"
	"github.com/apache/incubator-servicecomb-service-center/server/core/backend"
	pb "github.com/apache/incubator-servicecomb-service-center/server/core/proto"
	scerr "github.com/apache/incubator-servicecomb-service-center/server/error"
	"github.com/apache/incubator-servicecomb-service-center/server/infra/registry"
	"golang.org/x/net/context"
	"strconv"
	"strings"
	"time"
)

func GetLeaseId(ctx context.Context, domainProject string, serviceId string, instanceId string) (int64, error) {
	opts := append(FromContext(ctx),
		registry.WithStrKey(apt.GenerateInstanceLeaseKey(domainProject, serviceId, instanceId)))
	resp, err := backend.Store().Lease().Search(ctx, opts...)
	if err != nil {
		return -1, err
	}
	if len(resp.Kvs) <= 0 {
		return -1, nil
	}
	leaseID, _ := strconv.ParseInt(resp.Kvs[0].Value.(string), 10, 64)
	return leaseID, nil
}

func GetInstance(ctx context.Context, domainProject string, serviceId string, instanceId string) (*pb.MicroServiceInstance, error) {
	key := apt.GenerateInstanceKey(domainProject, serviceId, instanceId)
	opts := append(FromContext(ctx), registry.WithStrKey(key))

	resp, err := backend.Store().Instance().Search(ctx, opts...)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, nil
	}

	return resp.Kvs[0].Value.(*pb.MicroServiceInstance), nil
}

func ParseRevision(rev string) (int64, int64) {
	arrRev := strings.Split(rev, ".")
	reqRev, _ := strconv.ParseInt(arrRev[0], 10, 64)
	reqCount := int64(0)
	if len(arrRev) > 1 {
		reqCount, _ = strconv.ParseInt(arrRev[1], 10, 64)
	}
	return reqRev, reqCount
}

func FormatRevision(rev, count int64) string {
	return fmt.Sprintf("%d.%d", rev, count)
}

func GetAllInstancesOfOneService(ctx context.Context, domainProject string, serviceId string) ([]*pb.MicroServiceInstance, error) {
	key := apt.GenerateInstanceKey(domainProject, serviceId, "")
	opts := append(FromContext(ctx), registry.WithStrKey(key), registry.WithPrefix())
	resp, err := backend.Store().Instance().Search(ctx, opts...)
	if err != nil {
		util.Logger().Errorf(err, "Get instance of service %s from etcd failed.", serviceId)
		return nil, err
	}

	instances := make([]*pb.MicroServiceInstance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		instances = append(instances, kv.Value.(*pb.MicroServiceInstance))
	}
	return instances, nil
}

func GetInstanceCountOfOneService(ctx context.Context, domainProject string, serviceId string) (int64, error) {
	key := apt.GenerateInstanceKey(domainProject, serviceId, "")
	opts := append(FromContext(ctx),
		registry.WithStrKey(key),
		registry.WithPrefix(),
		registry.WithCountOnly())
	resp, err := backend.Store().Instance().Search(ctx, opts...)
	if err != nil {
		util.Logger().Errorf(err, "Get instance count of service %s from etcd failed.", serviceId)
		return 0, err
	}
	return resp.Count, nil
}

func InstanceExistById(ctx context.Context, domainProject string, serviceId string, instanceId string) (bool, error) {
	opts := append(FromContext(ctx),
		registry.WithStrKey(apt.GenerateInstanceKey(domainProject, serviceId, instanceId)),
		registry.WithCountOnly())
	resp, err := backend.Store().Instance().Search(ctx, opts...)
	if err != nil {
		return false, err
	}
	if resp.Count <= 0 {
		return false, nil
	}
	return true, nil
}

func InstanceExist(ctx context.Context, instance *pb.MicroServiceInstance) (string, *scerr.Error) {
	domainProject := util.ParseDomainProject(ctx)
	// check id index
	if len(instance.InstanceId) > 0 {
		exist, err := InstanceExistById(ctx, domainProject, instance.ServiceId, instance.InstanceId)
		if err != nil {
			return "", scerr.NewError(scerr.ErrInternal, err.Error())
		}
		if exist {
			return instance.InstanceId, nil
		}
	}
	return "", nil
}

type EndpointIndexValue struct {
	serviceId  string
	instanceId string
}

func ParseEndpointIndexValue(value []byte) EndpointIndexValue {
	endpointValue := EndpointIndexValue{}
	tmp := util.BytesToStringWithNoCopy(value)
	splitedTmp := strings.Split(tmp, "/")
	endpointValue.serviceId = splitedTmp[0]
	endpointValue.instanceId = splitedTmp[1]
	return endpointValue
}

func DeleteServiceAllInstances(ctx context.Context, serviceId string) error {
	domainProject := util.ParseDomainProject(ctx)

	instanceLeaseKey := apt.GenerateInstanceLeaseKey(domainProject, serviceId, "")
	resp, err := backend.Store().Lease().Search(ctx,
		registry.WithStrKey(instanceLeaseKey),
		registry.WithPrefix(),
		registry.WithNoCache())
	if err != nil {
		util.Logger().Errorf(err, "delete service %s all instance failed: get instance lease failed.", serviceId)
		return err
	}
	if resp.Count <= 0 {
		util.Logger().Warnf(nil, "service %s has no deployment of instance.", serviceId)
		return nil
	}
	for _, v := range resp.Kvs {
		leaseID, _ := strconv.ParseInt(v.Value.(string), 10, 64)
		backend.Registry().LeaseRevoke(ctx, leaseID)
	}
	return nil
}

func QueryAllProvidersInstances(ctx context.Context, selfServiceId string) (results []*pb.WatchInstanceResponse, rev int64) {
	results = []*pb.WatchInstanceResponse{}

	domainProject := util.ParseDomainProject(ctx)

	service, err := GetService(ctx, domainProject, selfServiceId)
	if err != nil {
		util.Logger().Errorf(err, "get service %s failed", selfServiceId)
		return
	}
	if service == nil {
		util.Logger().Errorf(nil, "service not exist, %s", selfServiceId)
		return
	}
	providerIds, _, err := GetAllProviderIds(ctx, domainProject, service)
	if err != nil {
		util.Logger().Errorf(err, "get service %s providers id set failed.", selfServiceId)
		return
	}

	rev = backend.Revision()

	for _, providerId := range providerIds {
		service, err := GetServiceWithRev(ctx, domainProject, providerId, rev)
		if err != nil {
			util.Logger().Errorf(err, "get service %s provider service %s file with revision %d failed.",
				selfServiceId, providerId, rev)
			return
		}
		if service == nil {
			continue
		}

		kvs, err := queryServiceInstancesKvs(ctx, providerId, rev)
		if err != nil {
			util.Logger().Errorf(err, "get service %s provider %s instances with revision %d failed.",
				selfServiceId, providerId, rev)
			return
		}

		for _, kv := range kvs {
			results = append(results, &pb.WatchInstanceResponse{
				Response: pb.CreateResponse(pb.Response_SUCCESS, "List instance successfully."),
				Action:   string(pb.EVT_INIT),
				Key: &pb.MicroServiceKey{
					Environment: service.Environment,
					AppId:       service.AppId,
					ServiceName: service.ServiceName,
					Version:     service.Version,
				},
				Instance: kv.Value.(*pb.MicroServiceInstance),
			})
		}
	}
	return
}

func queryServiceInstancesKvs(ctx context.Context, serviceId string, rev int64) ([]*backend.KeyValue, error) {
	domainProject := util.ParseDomainProject(ctx)
	key := apt.GenerateInstanceKey(domainProject, serviceId, "")
	resp, err := backend.Store().Instance().Search(ctx,
		registry.WithStrKey(key),
		registry.WithPrefix(),
		registry.WithRev(rev))
	if err != nil {
		util.Logger().Errorf(err, "query instance of service %s with revision %d from etcd failed.",
			serviceId, rev)
		return nil, err
	}
	return resp.Kvs, nil
}

func UpdateInstance(ctx context.Context, domainProject string, instance *pb.MicroServiceInstance) *scerr.Error {
	leaseID, err := GetLeaseId(ctx, domainProject, instance.ServiceId, instance.InstanceId)
	if err != nil {
		return scerr.NewError(scerr.ErrInternal, err.Error())
	}
	if leaseID == -1 {
		return scerr.NewError(scerr.ErrInstanceNotExists, "Instance's leaseId not exist.")
	}

	instance.ModTimestamp = strconv.FormatInt(time.Now().Unix(), 10)
	data, err := json.Marshal(instance)
	if err != nil {
		return scerr.NewError(scerr.ErrInternal, err.Error())
	}

	key := apt.GenerateInstanceKey(domainProject, instance.ServiceId, instance.InstanceId)

	resp, err := backend.Registry().TxnWithCmp(ctx,
		[]registry.PluginOp{registry.OpPut(
			registry.WithStrKey(key),
			registry.WithValue(data),
			registry.WithLease(leaseID))},
		[]registry.CompareOp{registry.OpCmp(
			registry.CmpVer(util.StringToBytesWithNoCopy(apt.GenerateServiceKey(domainProject, instance.ServiceId))),
			registry.CMP_NOT_EQUAL, 0)},
		nil)
	if err != nil {
		return scerr.NewError(scerr.ErrUnavailableBackend, err.Error())
	}
	if !resp.Succeeded {
		return scerr.NewError(scerr.ErrServiceNotExists, "Service does not exist.")
	}
	return nil
}
