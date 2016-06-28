# frakti

[![GoDoc](https://godoc.org/k8s.io/frakti?status.svg)](https://godoc.org/k8s.io/frakti) [![Build Status](https://travis-ci.org/kubernetes/frakti.svg?branch=master)](https://travis-ci.org/kubernetes/frakti)

Frakti lets Kubernetes run pods and containers directly inside hypervisors via [HyperContainer](http://hypercontainer.io/). It is light weighted and portable, but can provide much stronger isolation with independent kernel than linux-namespace-based container runtimes.

Frakti serves as a kubelet container runtime API server. Its endpoint should be configured while starting kubelet.

## Usage

Build frakti:

```sh
mkdir -p $GOPATH/src/k8s.io
git clone https://github.com/kubernetes/frakti.git $GOPATH/src/k8s.io/frakti
cd $GOPATH/src/k8s.io/frakti
make && make install
```

Start hyperd with gRPC endpoint `127.0.0.1:22318`:

```sh
$ grep gRPC /etc/hyper/config
gRPCHost=127.0.0.1:22318
```

Then start frakti:

```sh
frakti -v=3 --logtostderr --listen=127.0.0.1:10238 --hyper-endpoint=127.0.0.1:22318
```

Finaaly, start kubelet with frakti

```sh
kubelet --container-runtime-endpoint=127.0.0.1:10238 ...
```

## Documentation


Further information could be found at:

- [WIP: Kubelet container runtime API](https://github.com/kubernetes/kubernetes/tree/master/docs/proposals/runtime-client-server.md)
- [HyperContainer](http://hypercontainer.io/)
- [The blog on k8s.io about Hypernetesi](http://blog.kubernetes.io/2016/05/hypernetes-security-and-multi-tenancy-in-kubernetes.html)

## License

The work done has been licensed under Apache License 2.0.The license file can be found [here](LICENSE). You can find out more about license at [http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0).
