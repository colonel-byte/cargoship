#!/bin/sh
# Copyright 2026 colonel-byte
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

[ $(id -u) -eq 0 ] || exec sudo $0 $@

for bin in /var/lib/rancher/k3s/data/**/bin/; do
    [ -d $bin ] && export PATH=$PATH:$bin:$bin/aux
done

set -x

for service in /etc/systemd/system/k3s*.service; do
    [ -s $service ] && systemctl stop $(basename $service)
done

for service in /etc/init.d/k3s*; do
    [ -x $service ] && $service stop
done

pschildren() {
    ps -e -o ppid= -o pid= | \
    sed -e 's/^\s*//g; s/\s\s*/\t/g;' | \
    grep -w "^$1" | \
    cut -f2
}

pstree() {
    for pid in $@; do
        echo $pid
        for child in $(pschildren $pid); do
            pstree $child
        done
    done
}

killtree() {
    kill -9 $(
        { set +x; } 2>/dev/null;
        pstree $@;
        set -x;
    ) 2>/dev/null
}

remove_interfaces() {
    # Delete network interface(s) that match 'master cni0'
    ip link show 2>/dev/null | grep 'master cni0' | while read ignore iface ignore; do
        iface=${iface%%@*}
        [ -z "$iface" ] || ip link delete $iface
    done

    # Delete cni related interfaces
    ip link delete cni0
    ip link delete flannel.1
    ip link delete flannel-v6.1
    ip link delete kube-ipvs0
    ip link delete flannel-wg
    ip link delete flannel-wg-v6

    # Remove advertised routes in tailscale
    if [[ -n $(command -v tailscale) ]]; then
        tailscale set --advertise-routes=
    fi
}

getshims() {
    ps -e -o pid= -o args= | sed -e 's/^ *//; s/\s\s*/\t/;' | grep -w 'k3s/data/[^/]*/bin/containerd-shim' | cut -f1
}

killtree $({ set +x; } 2>/dev/null; getshims; set -x)

do_unmount() {
    awk -v path="$1" '$2 ~ ("^" path) { print $2 }' /proc/self/mounts | sort -r | xargs -r -t -n 1 umount
}

do_unmount '/run/k3s'
do_unmount '/var/lib/rancher/k3s'
do_unmount '/var/lib/kubelet/pods'
do_unmount '/run/netns/cni-'

remove_interfaces

rm -rf /var/lib/cni/
iptables-save | grep -v KUBE- | grep -v CNI- | grep -iv flannel | iptables-restore
ip6tables-save | grep -v KUBE- | grep -v CNI- | grep -iv flannel | ip6tables-restore
