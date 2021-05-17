// Copyright (c) 2021 Doc.ai and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clockmock

import (
	"time"

	libclock "github.com/benbjohnson/clock"
)

type mockTimer struct {
	mock  *Mock
	timer *libclock.Timer
}

func (t *mockTimer) C() <-chan time.Time {
	return t.timer.C
}

func (t *mockTimer) Stop() bool {
	t.mock.lock.RLock()
	defer t.mock.lock.RUnlock()

	return t.timer.Stop()
}

func (t *mockTimer) Reset(d time.Duration) bool {
	if d = safeDuration(d); d > 0 {
		t.mock.lock.RLock()
		defer t.mock.lock.RUnlock()
	} else {
		t.mock.lock.Lock()
		defer t.mock.lock.Unlock()

		defer t.mock.mock.Add(0)
	}

	return t.timer.Reset(d)
}
