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
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
	apt "github.com/apache/incubator-servicecomb-service-center/server/core"
	"github.com/apache/incubator-servicecomb-service-center/server/core/backend"
	pb "github.com/apache/incubator-servicecomb-service-center/server/core/proto"
	scerr "github.com/apache/incubator-servicecomb-service-center/server/error"
	"github.com/apache/incubator-servicecomb-service-center/server/infra/quota"
	"github.com/apache/incubator-servicecomb-service-center/server/infra/registry"
	"github.com/apache/incubator-servicecomb-service-center/server/plugin"
	"github.com/apache/incubator-servicecomb-service-center/server/service/cache"
	serviceUtil "github.com/apache/incubator-servicecomb-service-center/server/service/util"
	"golang.org/x/net/context"
	"math"
	"strconv"
	"time"
)

type InstanceService struct {
}

func (s *InstanceService) preProcessRegisterInstance(ctx context.Context, instance *pb.MicroServiceInstance) *scerr.Error {
	if len(instance.Status) == 0 {
		instance.Status = pb.MSI_UP
	}

	if len(instance.InstanceId) == 0 {
		instance.InstanceId = plugin.Plugins().UUID().GetInstanceId()
	}

	instance.Timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	instance.ModTimestamp = instance.Timestamp

	// 这里应该根据租约计时
	renewalInterval := apt.REGISTRY_DEFAULT_LEASE_RENEWALINTERVAL
	retryTimes := apt.REGISTRY_DEFAULT_LEASE_RETRYTIMES
	if instance.GetHealthCheck() == nil {
		instance.HealthCheck = &pb.HealthCheck{
			Mode:     pb.CHECK_BY_HEARTBEAT,
			Interval: renewalInterval,
			Times:    retryTimes,
		}
	} else {
		// Health check对象仅用于呈现服务健康检查逻辑，如果CHECK_BY_PLATFORM类型，表明由sidecar代发心跳，实例120s超时
		switch instance.HealthCheck.Mode {
		case pb.CHECK_BY_HEARTBEAT:
			if instance.HealthCheck.Interval <= 0 || instance.HealthCheck.Interval >= math.MaxInt32 ||
				instance.HealthCheck.Times <= 0 || instance.HealthCheck.Times >= math.MaxInt32 ||
				instance.HealthCheck.Interval*(instance.HealthCheck.Times+1) >= math.MaxInt32 {
				return scerr.NewError(scerr.ErrInvalidParams, "Invalid 'healthCheck' settings in request body.")
			}
		case pb.CHECK_BY_PLATFORM:
			// 默认120s
			instance.HealthCheck.Interval = renewalInterval
			instance.HealthCheck.Times = retryTimes
		}
	}

	domainProject := util.ParseDomainProject(ctx)
	service, err := serviceUtil.GetService(ctx, domainProject, instance.ServiceId)
	if service == nil || err != nil {
		return scerr.NewError(scerr.ErrServiceNotExists, "Invalid 'serviceId' in request body.")
	}
	instance.Version = service.Version
	return nil
}

func (s *InstanceService) Register(ctx context.Context, in *pb.RegisterInstanceRequest) (*pb.RegisterInstanceResponse, error) {
	remoteIP := util.GetIPFromContext(ctx)

	if err := Validate(in); err != nil {
		util.Logger().Errorf(err, "register instance failed, invalid parameters, operator %s.", remoteIP)
		return &pb.RegisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}

	instance := in.GetInstance()
	instanceFlag := util.StringJoin([]string{instance.ServiceId, instance.HostName}, "/")

	//允许自定义id
	//如果没填写 并且endpoints沒重復，則产生新的全局instance id
	oldInstanceId, checkErr := serviceUtil.InstanceExist(ctx, in.Instance)
	if checkErr != nil {
		util.Logger().Errorf(checkErr, "instance existence check failed, service %s, operator %s.",
			instanceFlag, remoteIP)
		resp := pb.CreateResponseWithSCErr(checkErr)
		if checkErr.InternalError() {
			return &pb.RegisterInstanceResponse{Response: resp}, checkErr
		}
		return &pb.RegisterInstanceResponse{Response: resp}, nil
	}
	if len(oldInstanceId) > 0 {
		util.Logger().Infof("register instance successful, reuse service %s instance %s, operator %s.",
			instance.ServiceId, oldInstanceId, remoteIP)
		return &pb.RegisterInstanceResponse{
			Response:   pb.CreateResponse(pb.Response_SUCCESS, "instance more exist."),
			InstanceId: oldInstanceId,
		}, nil
	}

	if err := s.preProcessRegisterInstance(ctx, instance); err != nil {
		util.Logger().Errorf(err, "register instance failed, service %s, operator %s.", instanceFlag, remoteIP)
		return &pb.RegisterInstanceResponse{
			Response: pb.CreateResponseWithSCErr(err),
		}, nil
	}

	//先以domain/project的方式组装
	domainProject := util.ParseDomainProject(ctx)

	var reporter quota.QuotaReporter
	if !apt.IsSCInstance(ctx) {
		res := quota.NewApplyQuotaResource(quota.MicroServiceInstanceQuotaType,
			domainProject, in.Instance.ServiceId, 1)
		rst := plugin.Plugins().Quota().Apply4Quotas(ctx, res)
		reporter = rst.Reporter
		err := rst.Err
		if reporter != nil {
			defer reporter.Close()
		}
		if err != nil {
			util.Logger().Errorf(err, "register instance failed, service %s, operator %s: no quota apply.",
				instanceFlag, remoteIP)
			response := &pb.RegisterInstanceResponse{
				Response: pb.CreateResponseWithSCErr(err),
			}
			if err.InternalError() {
				return response, err
			}
			return response, nil
		}
	}

	instanceId := instance.InstanceId
	data, err := json.Marshal(instance)
	if err != nil {
		util.Logger().Errorf(err,
			"register instance failed, service %s, instanceId %s, operator %s: json marshal data failed.",
			instanceFlag, instanceId, remoteIP)
		return &pb.RegisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}

	ttl := int64(instance.HealthCheck.Interval * (instance.HealthCheck.Times + 1))
	leaseID, err := backend.Registry().LeaseGrant(ctx, ttl)
	if err != nil {
		util.Logger().Errorf(err, "grant lease failed, instance %s, operator: %s.", instanceFlag, remoteIP)
		return &pb.RegisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrUnavailableBackend, err.Error()),
		}, err
	}
	util.Logger().Infof("lease grant %ds successfully, instance %s, operator: %s.", ttl, instanceFlag, remoteIP)

	// build the request options
	key := apt.GenerateInstanceKey(domainProject, instance.ServiceId, instanceId)
	hbKey := apt.GenerateInstanceLeaseKey(domainProject, instance.ServiceId, instanceId)

	opts := []registry.PluginOp{
		registry.OpPut(registry.WithStrKey(key), registry.WithValue(data),
			registry.WithLease(leaseID)),
		registry.OpPut(registry.WithStrKey(hbKey), registry.WithStrValue(fmt.Sprintf("%d", leaseID)),
			registry.WithLease(leaseID)),
	}

	resp, err := backend.Registry().TxnWithCmp(ctx, opts,
		[]registry.CompareOp{registry.OpCmp(
			registry.CmpVer(util.StringToBytesWithNoCopy(apt.GenerateServiceKey(domainProject, instance.ServiceId))),
			registry.CMP_NOT_EQUAL, 0)},
		nil)
	if err != nil {
		util.Logger().Errorf(err,
			"register instance failed, service %s, instanceId %s, operator %s: commit data into etcd failed.",
			instanceFlag, instanceId, remoteIP)
		return &pb.RegisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrUnavailableBackend, err.Error()),
		}, err
	}
	if !resp.Succeeded {
		util.Logger().Errorf(nil,
			"register instance failed, service %s, instanceId %s, operator %s: service does not exist.",
			instanceFlag, instanceId, remoteIP)
		return &pb.RegisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrServiceNotExists, "Service does not exist."),
		}, nil
	}

	if reporter != nil {
		if err := reporter.ReportUsedQuota(ctx); err != nil {
			util.Logger().Errorf(err,
				"register instance failed, service %s, instanceId %s, operator %s: report used quota failed.",
				instanceFlag, instanceId, remoteIP)
		}
	}
	util.Logger().Infof("register instance successful service %s, instanceId %s, operator %s.",
		instanceFlag, instanceId, remoteIP)
	return &pb.RegisterInstanceResponse{
		Response:   pb.CreateResponse(pb.Response_SUCCESS, "Register service instance successfully."),
		InstanceId: instanceId,
	}, nil
}

func (s *InstanceService) Unregister(ctx context.Context, in *pb.UnregisterInstanceRequest) (*pb.UnregisterInstanceResponse, error) {
	remoteIP := util.GetIPFromContext(ctx)

	if err := Validate(in); err != nil {
		util.Logger().Errorf(err, "unregister instance failed, invalid parameters, operator %s.", remoteIP)
		return &pb.UnregisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}

	domainProject := util.ParseDomainProject(ctx)
	serviceId := in.ServiceId
	instanceId := in.InstanceId

	instanceFlag := util.StringJoin([]string{serviceId, instanceId}, "/")

	isExist, err := serviceUtil.InstanceExistById(ctx, domainProject, serviceId, instanceId)
	if err != nil {
		util.Logger().Errorf(err, "unregister instance failed, instance %s, operator %s: query instance failed.", instanceFlag, remoteIP)
		return &pb.UnregisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	if !isExist {
		util.Logger().Errorf(nil, "unregister instance failed, instance %s, operator %s: instance not exist.", instanceFlag, remoteIP)
		return &pb.UnregisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInstanceNotExists, "Service instance does not exist."),
		}, nil
	}

	err, isInnerErr := revokeInstance(ctx, domainProject, serviceId, instanceId)
	if err != nil {
		util.Logger().Errorf(nil, "unregister instance failed, instance %s, operator %s: revoke instance failed.", instanceFlag, remoteIP)
		if isInnerErr {
			return &pb.UnregisterInstanceResponse{
				Response: pb.CreateResponse(scerr.ErrUnavailableBackend, err.Error()),
			}, err
		}
		return &pb.UnregisterInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInstanceNotExists, err.Error()),
		}, nil
	}

	util.Logger().Infof("unregister instance successful isntance %s, operator %s.", instanceFlag, remoteIP)
	return &pb.UnregisterInstanceResponse{
		Response: pb.CreateResponse(pb.Response_SUCCESS, "Unregister service instance successfully."),
	}, nil
}

func revokeInstance(ctx context.Context, domainProject string, serviceId string, instanceId string) (error, bool) {
	leaseID, err := serviceUtil.GetLeaseId(ctx, domainProject, serviceId, instanceId)
	if err != nil {
		return err, true
	}
	if leaseID == -1 {
		return errors.New("instance's leaseId not exist."), false
	}

	err = backend.Registry().LeaseRevoke(ctx, leaseID)
	if err != nil {
		return err, true
	}
	return nil, false
}

func (s *InstanceService) Heartbeat(ctx context.Context, in *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	remoteIP := util.GetIPFromContext(ctx)

	if err := Validate(in); err != nil {
		util.Logger().Errorf(err, "heartbeat failed, invalid parameters, operator %s.", remoteIP)
		return &pb.HeartbeatResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}

	domainProject := util.ParseDomainProject(ctx)
	instanceFlag := util.StringJoin([]string{in.ServiceId, in.InstanceId}, "/")

	_, ttl, err, isInnerErr := serviceUtil.HeartbeatUtil(ctx, domainProject, in.ServiceId, in.InstanceId)
	if err != nil {
		util.Logger().Errorf(err, "heartbeat failed, instance %s, internal error '%v'. operator: %s",
			instanceFlag, isInnerErr, remoteIP)
		if isInnerErr {
			return &pb.HeartbeatResponse{
				Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
			}, err
		}
		return &pb.HeartbeatResponse{
			Response: pb.CreateResponse(scerr.ErrInstanceNotExists, "Service instance does not exist."),
		}, nil
	}

	if ttl == 0 {
		util.Logger().Warnf(errors.New("connect backend timed out"),
			"heartbeat successful, but renew %s failed. operator: %s", instanceFlag, remoteIP)
	} else {
		util.Logger().Infof("heartbeat successful: %s renew ttl to %d. operator: %s", instanceFlag, ttl, remoteIP)
	}
	return &pb.HeartbeatResponse{
		Response: pb.CreateResponse(pb.Response_SUCCESS, "Update service instance heartbeat successfully."),
	}, nil
}

func (s *InstanceService) HeartbeatSet(ctx context.Context, in *pb.HeartbeatSetRequest) (*pb.HeartbeatSetResponse, error) {
	if len(in.Instances) == 0 {
		util.Logger().Errorf(nil, "heartbeats failed, invalid request. Body not contain Instances or is empty.")
		return &pb.HeartbeatSetResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, "Request format invalid."),
		}, nil
	}
	domainProject := util.ParseDomainProject(ctx)

	heartBeatCount := len(in.Instances)
	existFlag := make(map[string]bool, heartBeatCount)
	instancesHbRst := make(chan *pb.InstanceHbRst, heartBeatCount)
	noMultiCounter := 0
	for _, heartbeatElement := range in.Instances {
		if _, ok := existFlag[heartbeatElement.ServiceId+heartbeatElement.InstanceId]; ok {
			util.Logger().Warnf(nil, "heartbeat set %s/%s multiple", heartbeatElement.ServiceId, heartbeatElement.InstanceId)
			continue
		} else {
			existFlag[heartbeatElement.ServiceId+heartbeatElement.InstanceId] = true
			noMultiCounter++
		}
		util.Go(getHeartbeatFunc(ctx, domainProject, instancesHbRst, heartbeatElement))
	}
	count := 0
	successFlag := false
	failFlag := false
	instanceHbRstArr := make([]*pb.InstanceHbRst, 0, heartBeatCount)
	for heartbeat := range instancesHbRst {
		count++
		if len(heartbeat.ErrMessage) != 0 {
			failFlag = true
		} else {
			successFlag = true
		}
		instanceHbRstArr = append(instanceHbRstArr, heartbeat)
		if count == noMultiCounter {
			close(instancesHbRst)
		}
	}
	if !failFlag && successFlag {
		util.Logger().Infof("heartbeat set success")
		return &pb.HeartbeatSetResponse{
			Response:  pb.CreateResponse(pb.Response_SUCCESS, "Heartbeat set successfully."),
			Instances: instanceHbRstArr,
		}, nil
	} else {
		util.Logger().Errorf(nil, "heartbeat set failed, %v", in.Instances)
		return &pb.HeartbeatSetResponse{
			Response:  pb.CreateResponse(scerr.ErrInstanceNotExists, "Heartbeat set failed."),
			Instances: instanceHbRstArr,
		}, nil
	}
}

func getHeartbeatFunc(ctx context.Context, domainProject string, instancesHbRst chan<- *pb.InstanceHbRst, element *pb.HeartbeatSetElement) func(context.Context) {
	return func(_ context.Context) {
		hbRst := &pb.InstanceHbRst{
			ServiceId:  element.ServiceId,
			InstanceId: element.InstanceId,
			ErrMessage: "",
		}
		_, _, err, _ := serviceUtil.HeartbeatUtil(ctx, domainProject, element.ServiceId, element.InstanceId)
		if err != nil {
			hbRst.ErrMessage = err.Error()
			util.Logger().Errorf(err, "heartbeat set failed, %s/%s", element.ServiceId, element.InstanceId)
		}
		instancesHbRst <- hbRst
	}
}

func (s *InstanceService) GetOneInstance(ctx context.Context, in *pb.GetOneInstanceRequest) (*pb.GetOneInstanceResponse, error) {
	if err := Validate(in); err != nil {
		util.Logger().Errorf(err, "get instance failed: invalid parameters.")
		return &pb.GetOneInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}
	if checkErr := s.getInstancePreCheck(ctx, in.ProviderServiceId, in.ConsumerServiceId, in.Tags); checkErr != nil {
		util.Logger().Errorf(checkErr, "get instance failed: pre check failed.")
		resp := &pb.GetOneInstanceResponse{
			Response: pb.CreateResponseWithSCErr(checkErr),
		}
		if checkErr.InternalError() {
			return resp, checkErr
		}
		return resp, nil
	}
	conPro := util.StringJoin([]string{in.ConsumerServiceId, in.ProviderServiceId, in.ProviderInstanceId}, "/")

	serviceId := in.ProviderServiceId
	instanceId := in.ProviderInstanceId
	instance, err := serviceUtil.GetInstance(ctx, util.ParseTargetDomainProject(ctx), serviceId, instanceId)
	if err != nil {
		util.Logger().Errorf(err, "get instance failed, %s(consumer/provider): get instance failed.", conPro)
		return &pb.GetOneInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	if instance == nil {
		util.Logger().Errorf(nil, "get instance failed, %s(consumer/provider): instance not exist.", conPro)
		return &pb.GetOneInstanceResponse{
			Response: pb.CreateResponse(scerr.ErrInstanceNotExists, "Service instance does not exist."),
		}, nil
	}

	return &pb.GetOneInstanceResponse{
		Response: pb.CreateResponse(pb.Response_SUCCESS, "Get instance successfully."),
		Instance: instance,
	}, nil
}

func (s *InstanceService) getInstancePreCheck(ctx context.Context, providerServiceId, consumerServiceId string, tags []string) *scerr.Error {
	targetDomainProject := util.ParseTargetDomainProject(ctx)
	if !serviceUtil.ServiceExist(ctx, targetDomainProject, providerServiceId) {
		return scerr.NewError(scerr.ErrServiceNotExists, "Provider serviceId is invalid")
	}

	// Tag过滤
	if len(tags) > 0 {
		tagsFromETCD, err := serviceUtil.GetTagsUtils(ctx, targetDomainProject, providerServiceId)
		if err != nil {
			return scerr.NewErrorf(scerr.ErrInternal, "An error occurred in query provider tags(%s)", err.Error())
		}
		if len(tagsFromETCD) == 0 {
			return scerr.NewError(scerr.ErrTagNotExists, "Provider has no tag")
		}
		for _, tag := range tags {
			if _, ok := tagsFromETCD[tag]; !ok {
				return scerr.NewErrorf(scerr.ErrTagNotExists, "Provider tags do not contain '%s'", tag)
			}
		}
	}
	// 黑白名单
	// 跨应用调用
	return serviceUtil.Accessible(ctx, consumerServiceId, providerServiceId)
}

func (s *InstanceService) GetInstances(ctx context.Context, in *pb.GetInstancesRequest) (*pb.GetInstancesResponse, error) {
	if err := Validate(in); err != nil {
		util.Logger().Errorf(err, "get instances failed: invalid parameters.")
		return &pb.GetInstancesResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}
	if checkErr := s.getInstancePreCheck(ctx, in.ProviderServiceId, in.ConsumerServiceId, in.Tags); checkErr != nil {
		util.Logger().Errorf(checkErr, "get instances failed: pre check failed.")
		resp := &pb.GetInstancesResponse{
			Response: pb.CreateResponseWithSCErr(checkErr),
		}
		if checkErr.InternalError() {
			return resp, checkErr
		}
		return resp, nil
	}
	conPro := util.StringJoin([]string{in.ConsumerServiceId, in.ProviderServiceId}, "/")

	instances, err := serviceUtil.GetAllInstancesOfOneService(ctx, util.ParseTargetDomainProject(ctx), in.ProviderServiceId)
	if err != nil {
		util.Logger().Errorf(err, "get instances failed, %s(consumer/provider): get instances from etcd failed.", conPro)
		return &pb.GetInstancesResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	return &pb.GetInstancesResponse{
		Response:  pb.CreateResponse(pb.Response_SUCCESS, "Query service instances successfully."),
		Instances: instances,
	}, nil
}

func (s *InstanceService) Find(ctx context.Context, in *pb.FindInstancesRequest) (*pb.FindInstancesResponse, error) {
	err := Validate(in)
	if err != nil {
		util.Logger().Errorf(err, "find instance failed: invalid parameters.")
		return &pb.FindInstancesResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}

	domainProject := util.ParseDomainProject(ctx)
	findFlag := fmt.Sprintf("consumer %s find provider %s/%s/%s", in.ConsumerServiceId, in.AppId, in.ServiceName, in.VersionRule)

	service, err := serviceUtil.GetService(ctx, domainProject, in.ConsumerServiceId)
	if err != nil {
		util.Logger().Errorf(err, "find instance failed, %s: get consumer failed.", findFlag)
		return &pb.FindInstancesResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	if service == nil {
		util.Logger().Errorf(nil, "find instance failed, %s: consumer not exist.", findFlag)
		return &pb.FindInstancesResponse{
			Response: pb.CreateResponse(scerr.ErrServiceNotExists, "Consumer does not exist."),
		}, nil
	}

	provider := &pb.MicroServiceKey{
		Tenant:      util.ParseTargetDomainProject(ctx),
		Environment: service.Environment,
		AppId:       in.AppId,
		ServiceName: in.ServiceName,
		Alias:       in.ServiceName,
		Version:     in.VersionRule,
	}
	if apt.IsShared(provider) {
		// it means the shared micro-services must be the same env with SC.
		provider.Environment = apt.Service.Environment
		findFlag += "(provider is shared service in " + provider.Environment + " environment)"
	} else {
		// provider is not a shared micro-service,
		// only allow shared micro-service instances found in different domains.
		util.SetTargetDomainProject(ctx, util.ParseDomain(ctx), util.ParseProject(ctx))
		provider.Tenant = util.ParseTargetDomainProject(ctx)
	}

	noCache, cacheOnly := ctx.Value(serviceUtil.CTX_NOCACHE) == "1", ctx.Value(serviceUtil.CTX_CACHEONLY) == "1"
	rev, _ := ctx.Value(serviceUtil.CTX_REQUEST_REVISION).(string)
	reqRev, _ := serviceUtil.ParseRevision(rev)
	cloneCtx := util.CloneContext(ctx)

	// cache
	var (
		item           *cache.VersionRuleCacheItem
		i              = 0
		newVersionRule = !cache.DependencyRule.ExistVersionRule(cloneCtx, in.ConsumerServiceId, provider)
	)
	if noCache {
		i = 1
	}
	for ; i < 2; i++ {
		item, err = cache.FindInstances.Get(cloneCtx, service, provider, in.Tags)
		if err != nil {
			util.Logger().Errorf(err, "FindInstancesCache.Get failed, %s", findFlag)
			return &pb.FindInstancesResponse{
				Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
			}, err
		}
		if item == nil {
			mes := fmt.Sprintf("provider not exist, %s", findFlag)
			util.Logger().Errorf(nil, "FindInstancesCache.Get failed, %s", mes)
			return &pb.FindInstancesResponse{
				Response: pb.CreateResponse(scerr.ErrServiceNotExists, mes),
			}, nil
		}

		cacheRev, _ := serviceUtil.ParseRevision(item.Rev)
		if noCache || cacheOnly || reqRev <= cacheRev {
			break
		}
		util.SetContext(cloneCtx, serviceUtil.CTX_NOCACHE, "1")
	}

	// add dependency queue
	if newVersionRule {
		provider, err = s.reshapeProviderKey(ctx, provider, item.ServiceIds[0])
		if provider != nil {
			err = serviceUtil.AddServiceVersionRule(ctx, domainProject, service, provider)
		} else {
			mes := fmt.Sprintf("provider not exist, %s", findFlag)
			util.Logger().Errorf(nil, "AddServiceVersionRule failed, %s", mes)
			return &pb.FindInstancesResponse{
				Response: pb.CreateResponse(scerr.ErrServiceNotExists, mes),
			}, nil
		}
		if err != nil {
			util.Logger().Errorf(err, "AddServiceVersionRule failed, %s", findFlag)
			return &pb.FindInstancesResponse{
				Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
			}, err
		}
	}

	instances := item.Instances
	if rev == item.Rev {
		instances = instances[:0]
	}
	util.SetContext(ctx, serviceUtil.CTX_RESPONSE_REVISION, item.Rev)
	return &pb.FindInstancesResponse{
		Response:  pb.CreateResponse(pb.Response_SUCCESS, "Query service instances successfully."),
		Instances: instances,
	}, nil
}

func (s *InstanceService) reshapeProviderKey(ctx context.Context, provider *pb.MicroServiceKey, providerId string) (*pb.MicroServiceKey, error) {
	//维护version的规则,service name 可能是别名，所以重新获取
	providerService, err := serviceUtil.GetService(ctx, provider.Tenant, providerId)
	if providerService == nil {
		return nil, err
	}

	versionRule := provider.Version
	provider = pb.MicroServiceToKey(provider.Tenant, providerService)
	provider.Version = versionRule
	return provider, nil
}

func (s *InstanceService) UpdateStatus(ctx context.Context, in *pb.UpdateInstanceStatusRequest) (*pb.UpdateInstanceStatusResponse, error) {
	domainProject := util.ParseDomainProject(ctx)
	updateStatusFlag := util.StringJoin([]string{in.ServiceId, in.InstanceId, in.Status}, "/")
	if err := Validate(in); err != nil {
		util.Logger().Errorf(nil, "update instance status failed, %s.", updateStatusFlag)
		return &pb.UpdateInstanceStatusResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}

	instance, err := serviceUtil.GetInstance(ctx, domainProject, in.ServiceId, in.InstanceId)
	if err != nil {
		util.Logger().Errorf(err, "update instance status failed, %s: get instance from etcd failed.", updateStatusFlag)
		return &pb.UpdateInstanceStatusResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	if instance == nil {
		util.Logger().Errorf(nil, "update instance status failed, %s: instance not exist.", updateStatusFlag)
		return &pb.UpdateInstanceStatusResponse{
			Response: pb.CreateResponse(scerr.ErrInstanceNotExists, "Service instance does not exist."),
		}, nil
	}

	instance.Status = in.Status

	if err := serviceUtil.UpdateInstance(ctx, domainProject, instance); err != nil {
		util.Logger().Errorf(err, "update instance status failed, %s", updateStatusFlag)
		resp := &pb.UpdateInstanceStatusResponse{
			Response: pb.CreateResponseWithSCErr(err),
		}
		if err.InternalError() {
			return resp, err
		}
		return resp, nil
	}

	util.Logger().Infof("update instance status successful: %s.", updateStatusFlag)
	return &pb.UpdateInstanceStatusResponse{
		Response: pb.CreateResponse(pb.Response_SUCCESS, "Update service instance status successfully."),
	}, nil
}

func (s *InstanceService) UpdateInstanceProperties(ctx context.Context, in *pb.UpdateInstancePropsRequest) (*pb.UpdateInstancePropsResponse, error) {
	domainProject := util.ParseDomainProject(ctx)
	instanceFlag := util.StringJoin([]string{in.ServiceId, in.InstanceId}, "/")
	if err := Validate(in); err != nil {
		util.Logger().Errorf(nil, "update instance status failed, %s.", instanceFlag)
		return &pb.UpdateInstancePropsResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}

	instance, err := serviceUtil.GetInstance(ctx, domainProject, in.ServiceId, in.InstanceId)
	if err != nil {
		util.Logger().Errorf(err, "update instance properties failed, %s: get instance from etcd failed.", instanceFlag)
		return &pb.UpdateInstancePropsResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	if instance == nil {
		util.Logger().Errorf(nil, "update instance properties failed, %s: instance not exist.", instanceFlag)
		return &pb.UpdateInstancePropsResponse{
			Response: pb.CreateResponse(scerr.ErrInstanceNotExists, "Service instance does not exist."),
		}, nil
	}

	instance.Properties = in.Properties

	if err := serviceUtil.UpdateInstance(ctx, domainProject, instance); err != nil {
		util.Logger().Errorf(err, "update instance properties failed, %s", instanceFlag)
		resp := &pb.UpdateInstancePropsResponse{
			Response: pb.CreateResponseWithSCErr(err),
		}
		if err.InternalError() {
			return resp, err
		}
		return resp, nil
	}

	util.Logger().Infof("update instance properties successful: %s.", instanceFlag)
	return &pb.UpdateInstancePropsResponse{
		Response: pb.CreateResponse(pb.Response_SUCCESS, "Update service instance properties successfully."),
	}, nil
}

func (s *InstanceService) ClusterHealth(ctx context.Context) (*pb.GetInstancesResponse, error) {
	domainProject := util.StringJoin([]string{apt.REGISTRY_DOMAIN, apt.REGISTRY_PROJECT}, "/")
	serviceId, err := serviceUtil.GetServiceId(ctx, &pb.MicroServiceKey{
		AppId:       apt.Service.AppId,
		Environment: apt.Service.Environment,
		ServiceName: apt.Service.ServiceName,
		Version:     apt.Service.Version,
		Tenant:      domainProject,
	})

	if err != nil {
		util.Logger().Errorf(err, "health check failed: get service center serviceId failed.")
		return &pb.GetInstancesResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	if len(serviceId) == 0 {
		util.Logger().Errorf(nil, "health check failed: get service center serviceId not exist.")
		return &pb.GetInstancesResponse{
			Response: pb.CreateResponse(scerr.ErrServiceNotExists, "Service center serviceId not exist."),
		}, nil
	}

	instances, err := serviceUtil.GetAllInstancesOfOneService(ctx, domainProject, serviceId)
	if err != nil {
		util.Logger().Errorf(err, "health check failed: get service center instances failed.")
		return &pb.GetInstancesResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}
	return &pb.GetInstancesResponse{
		Response:  pb.CreateResponse(pb.Response_SUCCESS, "Health check successfully."),
		Instances: instances,
	}, nil
}
