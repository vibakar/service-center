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
package govern

import (
	"github.com/apache/incubator-servicecomb-service-center/pkg/util"
	apt "github.com/apache/incubator-servicecomb-service-center/server/core"
	"github.com/apache/incubator-servicecomb-service-center/server/core/backend"
	pb "github.com/apache/incubator-servicecomb-service-center/server/core/proto"
	scerr "github.com/apache/incubator-servicecomb-service-center/server/error"
	"github.com/apache/incubator-servicecomb-service-center/server/infra/registry"
	"github.com/apache/incubator-servicecomb-service-center/server/service"
	serviceUtil "github.com/apache/incubator-servicecomb-service-center/server/service/util"
	"golang.org/x/net/context"
)

var GovernServiceAPI pb.GovernServiceCtrlServerEx = &GovernService{}

type GovernService struct {
}

type ServiceDetailOpt struct {
	domainProject string
	service       *pb.MicroService
	countOnly     bool
	options       []string
}

func (governService *GovernService) GetServicesInfo(ctx context.Context, in *pb.GetServicesInfoRequest) (*pb.GetServicesInfoResponse, error) {
	util.SetContext(ctx, serviceUtil.CTX_CACHEONLY, "1")

	optionMap := make(map[string]struct{}, len(in.Options))
	for _, opt := range in.Options {
		optionMap[opt] = struct{}{}
	}

	options := make([]string, 0, len(optionMap))
	if _, ok := optionMap["all"]; ok {
		optionMap["statistics"] = struct{}{}
		options = []string{"tags", "rules", "instances", "schemas", "dependencies"}
	} else {
		for opt := range optionMap {
			options = append(options, opt)
		}
	}

	var st *pb.Statistics
	if _, ok := optionMap["statistics"]; ok {
		var err error
		st, err = statistics(ctx)
		if err != nil {
			return &pb.GetServicesInfoResponse{
				Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
			}, err
		}
		if len(optionMap) == 1 {
			return &pb.GetServicesInfoResponse{
				Response:   pb.CreateResponse(pb.Response_SUCCESS, "Statistics successfully."),
				Statistics: st,
			}, nil
		}
	}

	//获取所有服务
	services, err := serviceUtil.GetAllServiceUtil(ctx)
	if err != nil {
		util.Logger().Errorf(err, "Get all services for govern service failed.")
		return &pb.GetServicesInfoResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}

	allServiceDetails := make([]*pb.ServiceDetail, 0, len(services))
	domainProject := util.ParseDomainProject(ctx)
	for _, service := range services {
		if len(in.AppId) > 0 {
			if in.AppId != service.AppId {
				continue
			}
			if len(in.ServiceName) > 0 && in.ServiceName != service.ServiceName {
				continue
			}
		}

		serviceDetail, err := getServiceDetailUtil(ctx, ServiceDetailOpt{
			domainProject: domainProject,
			service:       service,
			countOnly:     in.CountOnly,
			options:       options,
		})
		if err != nil {
			return &pb.GetServicesInfoResponse{
				Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
			}, err
		}
		serviceDetail.MicroService = service
		allServiceDetails = append(allServiceDetails, serviceDetail)
	}

	return &pb.GetServicesInfoResponse{
		Response:          pb.CreateResponse(pb.Response_SUCCESS, "Get services info successfully."),
		AllServicesDetail: allServiceDetails,
		Statistics:        st,
	}, nil
}

func (governService *GovernService) GetServiceDetail(ctx context.Context, in *pb.GetServiceRequest) (*pb.GetServiceDetailResponse, error) {
	util.SetContext(ctx, serviceUtil.CTX_CACHEONLY, "1")

	domainProject := util.ParseDomainProject(ctx)
	options := []string{"tags", "rules", "instances", "schemas", "dependencies"}

	if len(in.ServiceId) == 0 {
		return &pb.GetServiceDetailResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, "Invalid request for getting service detail."),
		}, nil
	}

	service, err := serviceUtil.GetService(ctx, domainProject, in.ServiceId)
	if service == nil {
		return &pb.GetServiceDetailResponse{
			Response: pb.CreateResponse(scerr.ErrServiceNotExists, "Service does not exist."),
		}, nil
	}
	if err != nil {
		return &pb.GetServiceDetailResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}

	key := &pb.MicroServiceKey{
		Tenant:      domainProject,
		Environment: service.Environment,
		AppId:       service.AppId,
		ServiceName: service.ServiceName,
		Version:     "",
	}
	versions, err := getServiceAllVersions(ctx, key)
	if err != nil {
		util.Logger().Errorf(err, "Get service all version failed.")
		return &pb.GetServiceDetailResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}

	serviceInfo, err := getServiceDetailUtil(ctx, ServiceDetailOpt{
		domainProject: domainProject,
		service:       service,
		options:       options,
	})
	if err != nil {
		return &pb.GetServiceDetailResponse{
			Response: pb.CreateResponse(scerr.ErrInternal, err.Error()),
		}, err
	}

	serviceInfo.MicroService = service
	serviceInfo.MicroServiceVersions = versions
	return &pb.GetServiceDetailResponse{
		Response: pb.CreateResponse(pb.Response_SUCCESS, "Get service successfully."),
		Service:  serviceInfo,
	}, nil
}

func (governService *GovernService) GetApplications(ctx context.Context, in *pb.GetAppsRequest) (*pb.GetAppsResponse, error) {
	err := service.Validate(in)
	if err != nil {
		return &pb.GetAppsResponse{
			Response: pb.CreateResponse(scerr.ErrInvalidParams, err.Error()),
		}, nil
	}

	domainProject := util.ParseDomainProject(ctx)
	key := apt.GetServiceAppKey(domainProject, in.Environment, "")

	opts := append(serviceUtil.FromContext(ctx),
		registry.WithStrKey(key),
		registry.WithPrefix(),
		registry.WithKeyOnly())

	resp, err := backend.Store().ServiceIndex().Search(ctx, opts...)
	if err != nil {
		return nil, err
	}
	l := len(resp.Kvs)
	if l == 0 {
		return &pb.GetAppsResponse{
			Response: pb.CreateResponse(pb.Response_SUCCESS, "Get all applications successfully."),
		}, nil
	}

	apps := make([]string, 0, l)
	appMap := make(map[string]struct{}, l)
	for _, kv := range resp.Kvs {
		key := backend.GetInfoFromSvcIndexKV(kv)
		if _, ok := appMap[key.AppId]; ok {
			continue
		}
		appMap[key.AppId] = struct{}{}
		apps = append(apps, key.AppId)
	}

	return &pb.GetAppsResponse{
		Response: pb.CreateResponse(pb.Response_SUCCESS, "Get all applications successfully."),
		AppIds:   apps,
	}, nil
}

func getServiceAllVersions(ctx context.Context, serviceKey *pb.MicroServiceKey) ([]string, error) {
	versions := []string{}
	key := apt.GenerateServiceIndexKey(serviceKey)

	opts := append(serviceUtil.FromContext(ctx),
		registry.WithStrKey(key),
		registry.WithPrefix())

	resp, err := backend.Store().ServiceIndex().Search(ctx, opts...)
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Kvs) == 0 {
		return versions, nil
	}
	for _, kv := range resp.Kvs {
		key := backend.GetInfoFromSvcIndexKV(kv)
		versions = append(versions, key.Version)
	}
	return versions, nil
}

func getSchemaInfoUtil(ctx context.Context, domainProject string, serviceId string) ([]*pb.Schema, error) {
	key := apt.GenerateServiceSchemaKey(domainProject, serviceId, "")

	resp, err := backend.Store().Schema().Search(ctx,
		registry.WithStrKey(key),
		registry.WithPrefix())
	if err != nil {
		util.Logger().Errorf(err, "Get schema failed")
		return make([]*pb.Schema, 0), err
	}
	schemas := make([]*pb.Schema, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		schemaInfo := &pb.Schema{}
		schemaInfo.Schema = util.BytesToStringWithNoCopy(kv.Value.([]byte))
		schemaInfo.SchemaId = util.BytesToStringWithNoCopy(kv.Key[len(key):])
		schemas = append(schemas, schemaInfo)
	}
	return schemas, nil
}

func getServiceDetailUtil(ctx context.Context, serviceDetailOpt ServiceDetailOpt) (*pb.ServiceDetail, error) {
	serviceId := serviceDetailOpt.service.ServiceId
	options := serviceDetailOpt.options
	domainProject := serviceDetailOpt.domainProject
	serviceDetail := new(pb.ServiceDetail)
	if serviceDetailOpt.countOnly {
		serviceDetail.Statics = new(pb.Statistics)
	}

	for _, opt := range options {
		expr := opt
		switch expr {
		case "tags":
			tags, err := serviceUtil.GetTagsUtils(ctx, domainProject, serviceId)
			if err != nil {
				util.Logger().Errorf(err, "Get all tags for govern service failed.")
				return nil, err
			}
			serviceDetail.Tags = tags
		case "rules":
			rules, err := serviceUtil.GetRulesUtil(ctx, domainProject, serviceId)
			if err != nil {
				util.Logger().Errorf(err, "Get all rules for govern service failed.")
				return nil, err
			}
			for _, rule := range rules {
				rule.Timestamp = rule.ModTimestamp
			}
			serviceDetail.Rules = rules
		case "instances":
			if serviceDetailOpt.countOnly {
				instanceCount, err := serviceUtil.GetInstanceCountOfOneService(ctx, domainProject, serviceId)
				if err != nil {
					util.Logger().Errorf(err, "Get service's instances count for govern service failed.")
					return nil, err
				}
				serviceDetail.Statics.Instances = &pb.StInstance{
					Count: instanceCount}
				continue
			}
			instances, err := serviceUtil.GetAllInstancesOfOneService(ctx, domainProject, serviceId)
			if err != nil {
				util.Logger().Errorf(err, "Get service's all instances for govern service failed.")
				return nil, err
			}
			serviceDetail.Instances = instances
		case "schemas":
			schemas, err := getSchemaInfoUtil(ctx, domainProject, serviceId)
			if err != nil {
				util.Logger().Errorf(err, "Get service's all schemas for govern service failed.")
				return nil, err
			}
			serviceDetail.SchemaInfos = schemas
		case "dependencies":
			service := serviceDetailOpt.service
			dr := serviceUtil.NewDependencyRelation(ctx, domainProject, service, service)
			consumers, err := dr.GetDependencyConsumers(
				serviceUtil.WithoutSelfDependency(),
				serviceUtil.WithSameDomainProject())
			if err != nil {
				util.Logger().Errorf(err, "Get service's all consumers for govern service failed.")
				return nil, err
			}
			providers, err := dr.GetDependencyProviders(
				serviceUtil.WithoutSelfDependency(),
				serviceUtil.WithSameDomainProject())
			if err != nil {
				util.Logger().Errorf(err, "Get service's all providers for govern service failed.")
				return nil, err
			}

			serviceDetail.Consumers = consumers
			serviceDetail.Providers = providers
		case "":
			continue
		default:
			util.Logger().Errorf(nil, "option %s from request is invalid.", opt)
		}
	}
	return serviceDetail, nil
}

func statistics(ctx context.Context) (*pb.Statistics, error) {
	result := &pb.Statistics{
		Services:  &pb.StService{},
		Instances: &pb.StInstance{},
		Apps:      &pb.StApp{},
	}
	domainProject := util.ParseDomainProject(ctx)
	opts := serviceUtil.FromContext(ctx)

	// services
	key := apt.GetServiceIndexRootKey(domainProject) + "/"
	svcOpts := append(opts,
		registry.WithStrKey(key),
		registry.WithPrefix())
	respSvc, err := backend.Store().ServiceIndex().Search(ctx, svcOpts...)
	if err != nil {
		return nil, err
	}

	app := make(map[string]struct{}, respSvc.Count)
	svcWithNonVersion := make(map[string]struct{}, respSvc.Count)
	svcIdToNonVerKey := make(map[string]string, respSvc.Count)
	for _, kv := range respSvc.Kvs {
		key := backend.GetInfoFromSvcIndexKV(kv)
		if _, ok := app[key.AppId]; !ok {
			app[key.AppId] = struct{}{}
		}

		key.Version = ""
		svcWithNonVersionKey := apt.GenerateServiceIndexKey(key)
		if _, ok := svcWithNonVersion[svcWithNonVersionKey]; !ok {
			svcWithNonVersion[svcWithNonVersionKey] = struct{}{}
		}
		svcIdToNonVerKey[kv.Value.(string)] = svcWithNonVersionKey
	}

	result.Services.Count = int64(len(svcWithNonVersion))
	result.Apps.Count = int64(len(app))

	respGetInstanceCountByDomain := make(chan GetInstanceCountByDomainResponse, 1)
	util.Go(func(_ context.Context) {
		getInstanceCountByDomain(ctx, respGetInstanceCountByDomain)
	})

	// instance
	key = apt.GetInstanceRootKey(domainProject) + "/"
	instOpts := append(opts,
		registry.WithStrKey(key),
		registry.WithPrefix(),
		registry.WithKeyOnly())
	respIns, err := backend.Store().Instance().Search(ctx, instOpts...)
	if err != nil {
		return nil, err
	}

	onlineServices := make(map[string]struct{}, respSvc.Count)
	for _, kv := range respIns.Kvs {
		serviceId, _, _ := backend.GetInfoFromInstKV(kv)
		key, ok := svcIdToNonVerKey[serviceId]
		if !ok {
			continue
		}
		if _, ok := onlineServices[key]; !ok {
			onlineServices[key] = struct{}{}
		}
	}
	result.Instances.Count = respIns.Count
	result.Services.OnlineCount = int64(len(onlineServices))

	data := <-respGetInstanceCountByDomain
	close(respGetInstanceCountByDomain)
	if data.err != nil {
		return nil, data.err
	}
	result.Instances.CountByDomain = data.countByDomain
	return result, err
}

type GetInstanceCountByDomainResponse struct {
	err           error
	countByDomain int64
}

func getInstanceCountByDomain(ctx context.Context, resp chan GetInstanceCountByDomainResponse) {
	domainId := util.ParseDomain(ctx)
	key := apt.GetInstanceRootKey(domainId) + "/"
	instOpts := append([]registry.PluginOpOption{},
		registry.WithStrKey(key),
		registry.WithPrefix(),
		registry.WithKeyOnly(),
		registry.WithCountOnly())
	respIns, err := backend.Store().Instance().Search(ctx, instOpts...)
	if err != nil {
		util.Logger().Errorf(err, "get instance count under same domainId %s", domainId)
	}
	resp <- GetInstanceCountByDomainResponse{
		err:           err,
		countByDomain: respIns.Count,
	}
}
