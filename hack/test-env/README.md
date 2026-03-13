# Single-Node Kubernetes KVM Test Environment

Ansible + Podman automation for spinning up a KVM virtual machine with a single-node Kubernetes cluster using [KinD](https://kind.sigs.k8s.io/) (Kubernetes in Docker). Used for local testing of the Kea DHCP operator.

## Prerequisites

- A **KVM host** with libvirt, QEMU, and `genisoimage` installed
- **SSH access** to the KVM host (key or password)
- **Podman** on the machine running the playbooks (for containerized runner)
- ~70GB free disk space on the KVM host

## Quick Start

### 1. Configure the KVM host

Edit `inventory/hosts` to point at your KVM host:

```ini
[kvm_hosts]
kvm-host ansible_host=192.168.1.100 ansible_user=root ansible_ssh_private_key_file=/workspace/ssh-key
```

### 2. Set up SSH authentication

```bash
# Option A: Copy your SSH key
cp ~/.ssh/id_rsa ssh-key && chmod 600 ssh-key
ssh-keygen -y -f ssh-key > ssh-key.pub

# Option B: Use password
export KVM_HOST_PASSWORD='your-password'
```

### 3. Deploy

```bash
# Using the runner script (containerized Ansible)
./ansible-runner.sh deploy

# Or using Ansible directly
ansible-galaxy collection install -r requirements.yml
ansible-playbook deploy-k8s.yml
```

### 4. Use the cluster

```bash
export KUBECONFIG=$(pwd)/artifacts/k8s-test/kubeconfig
kubectl get nodes
kubectl get crds | grep kea
```

### 5. Teardown

```bash
./ansible-runner.sh destroy --yes
```

## What Gets Created

| Resource | Details |
|----------|---------|
| KVM VM | CentOS Stream 9, 8GB RAM, 4 vCPU, 60GB disk |
| Container Runtime | Podman (inside VM) |
| KinD Cluster | Single control-plane node |
| Kubernetes | v1.32.3 (configurable) |
| Namespaces | `kea-system`, `kea-dhcp-operator` |
| CRDs | All Kea DHCP operator CRDs |
| Kubeconfig | `artifacts/k8s-test/kubeconfig` (patched with VM IP) |

## Configuration

Edit `inventory/group_vars/all.yml` to customize:

| Variable | Default | Description |
|----------|---------|-------------|
| `vm_name` | `k8s-test` | KVM VM name |
| `vm_memory` | `8192` | VM RAM in MB |
| `vm_cpus` | `4` | VM vCPUs |
| `vm_disk_size` | `60` | OS disk in GB |
| `vm_network_type` | `network` | `network`, `bridge`, or `direct` |
| `k8s_version` | `v1.32.3` | Kubernetes version |
| `kind_version` | `v0.27.0` | KinD version |
| `cluster_name` | `kea-test` | KinD cluster name |
| `vm_user` | `k8s-admin` | VM SSH user |
| `vm_password` | `k8s-test-123` | VM SSH password |

## Directory Structure

```
hack/test-env/
├── ansible-runner.sh          # Podman-based runner script
├── ansible.cfg                # Ansible configuration
├── Containerfile              # Ansible runner container image
├── deploy-k8s.yml            # Deploy playbook (create VM + install K8s)
├── destroy-k8s.yml           # Teardown playbook (destroy VM)
├── check-status.yml          # Status check playbook
├── requirements.yml          # Ansible collection dependencies
├── inventory/
│   ├── hosts                 # KVM host + VM definitions
│   └── group_vars/
│       └── all.yml           # Global variables
├── roles/
│   ├── k8s_prerequisites/    # Validates KVM host (libvirt, disk, etc.)
│   ├── vm_create/            # Creates KVM VM with cloud-init
│   │   ├── tasks/main.yml
│   │   └── templates/
│   │       ├── meta-data.j2
│   │       ├── network-config.j2
│   │       ├── user-data.j2
│   │       └── vm-definition.xml.j2
│   ├── k8s_install/          # Installs Podman, KinD, kubectl in VM
│   │   ├── tasks/main.yml
│   │   └── templates/
│   │       └── kind-config.yaml.j2
│   └── k8s_configure/        # Installs CRDs, creates namespaces
└── artifacts/                # Generated kubeconfig and cluster info
```

## Workflow

```
KVM Host                          VM (k8s-test)
────────                          ──────────────
1. Download CentOS cloud image
2. Create qcow2 disk
3. Generate cloud-init ISO
4. Define + start VM
                                  5. cloud-init: user, SSH, guest agent
                                  6. Install podman, KinD, kubectl
                                  7. kind create cluster
                                  8. kubectl create ns kea-system
                                  9. kubectl apply CRDs
← kubeconfig fetched + patched with VM IP
```

## Runner Script Commands

```bash
./ansible-runner.sh build          # Build the Ansible container image
./ansible-runner.sh deploy         # Create VM + install K8s
./ansible-runner.sh deploy -v      # Verbose output
./ansible-runner.sh status         # Check VM + cluster status
./ansible-runner.sh destroy        # Destroy VM (with confirmation)
./ansible-runner.sh destroy --yes  # Destroy VM (skip confirmation)
./ansible-runner.sh shell          # Open shell in Ansible container
./ansible-runner.sh run <playbook> # Run any playbook
```
