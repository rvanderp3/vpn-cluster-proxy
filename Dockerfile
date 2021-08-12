FROM quay.io/centos/amd64:centos8

COPY bin/vpn-cluster-proxy-webhook .

CMD ./vpn-cluster-proxy-webhook