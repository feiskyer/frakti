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

package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/frakti/pkg/manager"
)

const (
	kubeHyperVersion = "0.1"
)

var (
	version = flag.Bool("version", false, "Print version and exit")
	listen  = flag.String("listen", "127.0.0.1:10238",
		"Which port to listen on, e.g. 127.0.0.1:10238")
	hyperEndpoint = flag.String("hyper-endpoint", "127.0.0.1:22318",
		"The endpoint for connecting hyperd, e.g. 127.0.0.1:22318")
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("frakti version: %s\n", kubeHyperVersion)
		os.Exit(0)
	}

	server, err := manager.NewKubeHyperManager(*hyperEndpoint)
	if err != nil {
		fmt.Println("Initialize frakti server failed: ", err)
		os.Exit(1)
	}

	fmt.Println(server.Serve(*listen))
}
