/*
Copyright 2016 The Kubernetes Authors All rights reserved.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manager

import (
	"time"
)

// inList checks if a string is in a list
func inList(in string, list []string) bool {
	for _, str := range list {
		if in == str {
			return true
		}
	}

	return false
}

// inMap checks if a map is in dest map
func inMap(in, dest map[string]string) bool {
	for k, v := range in {
		if value, ok := dest[k]; ok {
			if value != v {
				return false
			}
		} else {
			return false
		}
	}

	return true
}

func parseTimeString(str string) (int64, error) {
	t := time.Date(0, 0, 0, 0, 0, 0, 0, time.Local)
	if str == "" {
		return t.Unix(), nil
	}

	layout := "2006-01-02T15:04:05Z"
	t, err := time.Parse(layout, str)
	if err != nil {
		return t.Unix(), err
	}

	return t.Unix(), nil
}
