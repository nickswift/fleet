// Copyright 2014 The fleet Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkg

import (
	"net/http"

	"github.com/nickswift/fleet/log"
)

type LoggingHTTPTransport struct {
	http.Transport
}

func (lt *LoggingHTTPTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	log.Debugf("HTTP %s %s", req.Method, req.URL.String())
	resp, err = lt.Transport.RoundTrip(req)
	if err == nil {
		log.Debugf("HTTP %s %s %s", req.Method, req.URL.String(), resp.Status)
	}
	return
}
