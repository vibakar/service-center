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
package backend

import "sync"

var (
	EventProxies map[StoreType]*KvEventProxy
)

func init() {
	EventProxies = make(map[StoreType]*KvEventProxy, typeEnd)
	for i := StoreType(0); i != typeEnd; i++ {
		EventProxies[i] = NewEventProxy()
	}
}

type KvEventProxy struct {
	evtHandleFuncs []KvEventFunc
	lock           sync.RWMutex
}

func (h *KvEventProxy) AddHandleFunc(f KvEventFunc) {
	h.lock.Lock()
	h.evtHandleFuncs = append(h.evtHandleFuncs, f)
	h.lock.Unlock()
}

func (h *KvEventProxy) OnEvent(evt KvEvent) {
	h.lock.RLock()
	for _, f := range h.evtHandleFuncs {
		f(evt)
	}
	h.lock.RUnlock()
}

func NewEventProxy() *KvEventProxy {
	return &KvEventProxy{}
}

func EventProxy(t StoreType) *KvEventProxy {
	return EventProxies[t]
}
