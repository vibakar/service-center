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

import "testing"

func TestSubject_Fetch(t *testing.T) {
	s := NewSubject("s1")
	if s.Name() != "s1" {
		t.Fatalf("TestSubject_Fetch failed")
	}
	g := s.GetOrNewGroup("g1")
	if g == nil {
		t.Fatalf("TestSubject_Fetch failed")
	}
	if s.GetOrNewGroup(g.Name()) != g {
		t.Fatalf("TestSubject_Fetch failed")
	}
	o := s.GetOrNewGroup("g2")
	if s.Groups("g2") != o {
		t.Fatalf("TestSubject_Fetch failed")
	}
	if s.Size() != 2 {
		t.Fatalf("TestSubject_Fetch failed")
	}
	s.Remove(o.Name())
	if s.Groups("g2") != nil {
		t.Fatalf("TestSubject_Fetch failed")
	}
	if s.Size() != 1 {
		t.Fatalf("TestSubject_Fetch failed")
	}
	mock1 := &mockSubscriber{BaseSubscriber: NewSubscriber(INSTANCE, "s1", "g1")}
	mock2 := &mockSubscriber{BaseSubscriber: NewSubscriber(INSTANCE, "s1", "g2")}
	g.AddSubscriber(mock1)
	job := &BaseNotifyJob{group: "g3"}
	s.Notify(job)
	if mock1.job != nil || mock2.job != nil {
		t.Fatalf("TestSubject_Fetch failed")
	}
	job.group = "g1"
	s.Notify(job)
	if mock1.job != job || mock2.job != nil {
		t.Fatalf("TestSubject_Fetch failed")
	}
	job.group = ""
	s.Notify(job)
	if mock1.job != job && mock2.job != job {
		t.Fatalf("TestSubject_Fetch failed")
	}
}
