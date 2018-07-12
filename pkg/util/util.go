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
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
	"unsafe"
)

func MinInt(x, y int) int {
	if x <= y {
		return x
	} else {
		return y
	}
}

func ClearStringMemory(src *string) {
	p := (*struct {
		ptr uintptr
		len int
	})(unsafe.Pointer(src))

	l := MinInt(p.len, 32)
	ptr := p.ptr
	for idx := 0; idx < l; idx = idx + 1 {
		b := (*byte)(unsafe.Pointer(ptr))
		*b = 0
		ptr += 1
	}
}

func ClearByteMemory(src []byte) {
	l := MinInt(len(src), 32)
	for idx := 0; idx < l; idx = idx + 1 {
		src[idx] = 0
	}
}

func DeepCopy(dst, src interface{}) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(src); err != nil {
		return err
	}
	return gob.NewDecoder(bytes.NewBuffer(buf.Bytes())).Decode(dst)
}

func SafeCloseChan(c chan struct{}) {
	select {
	case _, ok := <-c:
		if ok {
			close(c)
		}
	default:
		close(c)
	}
}

func BytesToStringWithNoCopy(bytes []byte) string {
	return *(*string)(unsafe.Pointer(&bytes))
}

func StringToBytesWithNoCopy(s string) []byte {
	x := (*[2]uintptr)(unsafe.Pointer(&s))
	h := [3]uintptr{x[0], x[1], x[1]}
	return *(*[]byte)(unsafe.Pointer(&h))
}

func ListToMap(list []string) map[string]struct{} {
	ret := make(map[string]struct{}, len(list))
	for _, v := range list {
		ret[v] = struct{}{}
	}
	return ret
}

func MapToList(dict map[string]struct{}) []string {
	ret := make([]string, 0, len(dict))
	for k := range dict {
		ret = append(ret, k)
	}
	return ret
}

func StringJoin(args []string, sep string) string {
	l := len(args)
	switch l {
	case 0:
		return ""
	case 1:
		return args[0]
	default:
		n := len(sep) * (l - 1)
		for i := 0; i < l; i++ {
			n += len(args[i])
		}
		b := make([]byte, n)
		sl := copy(b, args[0])
		for i := 1; i < l; i++ {
			sl += copy(b[sl:], sep)
			sl += copy(b[sl:], args[i])
		}
		return BytesToStringWithNoCopy(b)
	}
}

func RecoverAndReport() (r interface{}) {
	if r = recover(); r != nil {
		LogPanic(r)
	}
	return
}

// this function can only be called in recover().
func LogPanic(args ...interface{}) {
	for i := 2; i < 10; i++ {
		file, method, line, ok := GetCaller(i)
		if !ok {
			break
		}

		if strings.Index(file, "service-center") > 0 || strings.Index(file, "servicecenter") > 0 {
			Logger().Errorf(nil, "recover from %s %s():%d! %s", FileLastName(file), method, line, fmt.Sprint(args...))
			return
		}
	}

	file, method, line, _ := GetCaller(0)
	fmt.Fprintln(os.Stderr, time.Now().Format("2006-01-02T15:04:05.000Z07:00"), "FATAL", "system", os.Getpid(),
		fmt.Sprintf("%s %s():%d", FileLastName(file), method, line), fmt.Sprint(args...))
	fmt.Fprintln(os.Stderr, BytesToStringWithNoCopy(debug.Stack()))
}

func FileLastName(file string) string {
	if sp1 := strings.LastIndex(file, "/"); sp1 >= 0 {
		if sp2 := strings.LastIndex(file[:sp1], "/"); sp2 >= 0 {
			file = file[sp2+1:]
		}
	}
	return file
}

func GetCaller(skip int) (string, string, int, bool) {
	pc, file, line, ok := runtime.Caller(skip + 1)
	method := FormatFuncName(runtime.FuncForPC(pc).Name())
	return file, method, line, ok
}

func Int16ToInt64(bs []int16) (in int64) {
	l := len(bs)
	if l > 4 || l == 0 {
		return 0
	}

	pi := (*[4]int16)(unsafe.Pointer(&in))
	if IsBigEndian() {
		for i := range bs {
			pi[i] = bs[l-i-1]
		}
		return
	}

	for i := range bs {
		pi[3-i] = bs[l-i-1]
	}
	return
}

func FormatFuncName(f string) string {
	i := strings.LastIndex(f, "/")
	j := strings.Index(f[i+1:], ".")
	if j < 1 {
		return "???"
	}
	_, fun := f[:i+j+1], f[i+j+2:]
	i = strings.LastIndex(fun, ".")
	return fun[i+1:]
}

func FuncName(f interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
}

func SliceHave(arr []string, str string) bool {
	for _, item := range arr {
		if item == str {
			return true
		}
	}
	return false
}

// do not call after drain timer.C channel
func ResetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		// timer is expired: can not find the timer in timer stack
		// select {
		// case <-timer.C:
		// 	// here block when drain channel call before timer.Stop()
		// default:
		// 	// here will cause a BUG When sendTime() after drain channel
		// 	// BUG: timer.C still trigger even after timer.Reset()
		// }
		<-timer.C
	}
	timer.Reset(d)
}
