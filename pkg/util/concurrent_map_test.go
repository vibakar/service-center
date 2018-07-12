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
	"testing"
)

func TestConcurrentMap(t *testing.T) {
	cm := ConcurrentMap{}
	s := cm.Size()
	if s != 0 {
		t.Fatalf("TestConcurrentMap Size failed.")
	}
	v, b := cm.Get("a")
	if b || v != nil {
		t.Fatalf("TestConcurrentMap Get a not exist item failed.")
	}
	v = cm.Put("a", "1")
	if v != nil {
		t.Fatalf("TestConcurrentMap Put a new item failed.")
	}
	v, b = cm.Get("a")
	if !b || v.(string) != "1" {
		t.Fatalf("TestConcurrentMap Get an exist item failed.")
	}
	v = cm.Put("a", "2")
	if v.(string) != "1" {
		t.Fatalf("TestConcurrentMap Put an item again failed.")
	}
	cm.PutIfAbsent("b", "1")
	cm.PutIfAbsent("a", "3")
	v, b = cm.Get("a")
	if !b || v.(string) != "2" {
		t.Fatalf("TestConcurrentMap Get an item after PutIfAbsent failed.")
	}
	v = cm.Remove("a")
	if v.(string) != "2" {
		t.Fatalf("TestConcurrentMap Remove an item failed.")
	}
	v, b = cm.Get("a")
	if b || v != nil {
		t.Fatalf("TestConcurrentMap Get an item after Remove failed.")
	}
	s = cm.Size()
	if s != 1 { // only 'b' is left
		t.Fatalf("TestConcurrentMap Size after Put failed.")
	}
	cm.Clear()
	s = cm.Size()
	if s != 0 {
		t.Fatalf("TestConcurrentMap Size after Clear failed.")
	}
}

func TestConcurrentMap_ForEach(t *testing.T) {
	l := 0
	cm := ConcurrentMap{}
	cm.ForEach(func(item MapItem) bool {
		l++
		return true
	})
	if l != 0 {
		t.Fatalf("TestConcurrentMap_ForEach failed.")
	}
	for i := 0; i < 1000; i++ {
		cm.Put(i, i)
	}
	cm.ForEach(func(item MapItem) bool {
		l++
		cm.Remove(item.Key)
		return true
	})
	if l != 1000 || cm.Size() != 0 {
		t.Fatalf("TestConcurrentMap_ForEach does not empty failed.")
	}
}

func TestNewConcurrentMap(t *testing.T) {
	cm := NewConcurrentMap(100)
	if cm.size != 100 {
		t.Fatalf("TestNewConcurrentMap failed.")
	}
}

func BenchmarkConcurrentMap_Get(b *testing.B) {
	var v interface{}
	cm := ConcurrentMap{}
	cm.Put("a", "1")
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			v, _ = cm.Get("a")
		}
	})
	b.ReportAllocs()
	// 20000000	        88.7 ns/op	       0 B/op	       0 allocs/op
}

func BenchmarkConcurrentMap_Put(b *testing.B) {
	cm := &ConcurrentMap{}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cm.Put("a", "1")
		}
	})
	b.ReportAllocs()
	// 3000000	       420 ns/op	      32 B/op	       2 allocs/op
}

func BenchmarkConcurrentMap_PutAndGet(b *testing.B) {
	var v interface{}
	cm := &ConcurrentMap{}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cm.Put("a", "1")
			v, _ = cm.Get("a")
		}
	})
	b.ReportAllocs()
	// 3000000	       560 ns/op	      32 B/op	       2 allocs/op
}

func BenchmarkConcurrentMap_ForEach(b *testing.B) {
	cm := ConcurrentMap{}
	for i := 0; i < 100; i++ {
		cm.Put(i, i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cm.ForEach(func(item MapItem) bool {
				return true
			})
		}
	})
	b.ReportAllocs()
	// 500000	      3148 ns/op	    3296 B/op	       2 allocs/op
}
