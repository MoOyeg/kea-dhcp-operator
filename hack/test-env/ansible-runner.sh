#!/bin/bash
# Run Ansible playbooks using Podman container
# This eliminates the need to install Ansible on the host

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="localhost/ansible-k8s-runner:latest"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored messages
print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if podman is installed
if ! command -v podman &> /dev/null; then
    print_error "Podman is required but not installed"
    echo "Install podman:"
    echo "  RHEL/Fedora: sudo dnf install -y podman"
    echo "  Ubuntu/Debian: sudo apt install -y podman"
    exit 1
fi

# Build the Ansible container image if it doesn't exist
build_image() {
    print_info "Checking for Ansible container image..."

    if podman image exists "$IMAGE_NAME"; then
        print_info "Ansible container image already exists"
        return 0
    fi

    print_info "Building Ansible container image..."
    cd "$SCRIPT_DIR"

    if [ -f "Containerfile" ]; then
        podman build -t "$IMAGE_NAME" -f Containerfile .
        print_info "Image built successfully"
    else
        print_error "Containerfile not found in $SCRIPT_DIR"
        exit 1
    fi
}

# Function to run Ansible playbook in container
run_ansible() {
    local playbook="$1"
    shift
    local extra_args="$@"

    if [ ! -f "$SCRIPT_DIR/$playbook" ]; then
        print_error "Playbook not found: $playbook"
        exit 1
    fi

    # Check for SSH authentication
    if [ ! -f "$SCRIPT_DIR/ssh-key" ] && [ -z "$KVM_HOST_PASSWORD" ]; then
        print_error "No SSH authentication configured!"
        echo ""
        echo "You need SSH access to the KVM host. Choose one option:"
        echo ""
        echo "Option 1 - SSH Key (Recommended):"
        if [ -f "$HOME/.ssh/id_rsa" ]; then
            echo "  Copy your key: cp ~/.ssh/id_rsa ssh-key && chmod 600 ssh-key"
        else
            echo "  Generate key: ssh-keygen -t rsa -b 4096"
            echo "  Copy to KVM host: ssh-copy-id root@<kvm-host>"
            echo "  Copy here: cp ~/.ssh/id_rsa ssh-key && chmod 600 ssh-key"
        fi
        echo ""
        echo "Option 2 - Password:"
        echo "  export KVM_HOST_PASSWORD='your-password'"
        echo ""
        exit 1
    fi

    print_info "Running playbook: $playbook"

    # Prepare volume mounts
    local volumes=(
        "-v" "$SCRIPT_DIR:/workspace:Z"
        "-v" "$SCRIPT_DIR/../..:/repo:Z,ro"
    )

    # Mount SSH key if it exists
    if [ -f "$SCRIPT_DIR/ssh-key" ]; then
        volumes+=("-v" "$SCRIPT_DIR/ssh-key:/workspace/ssh-key:Z")
    fi

    # Pass through environment variables
    local env_vars=()

    # Add KVM host SSH password if set
    if [ -n "$KVM_HOST_PASSWORD" ]; then
        env_vars+=("-e" "KVM_HOST_PASSWORD=${KVM_HOST_PASSWORD}")
    fi

    # Run the container
    podman run --rm -it \
        "${volumes[@]}" \
        "${env_vars[@]}" \
        --network host \
        "$IMAGE_NAME" \
        -i /workspace/inventory/hosts \
        "$playbook" \
        $extra_args
}

# Show usage
usage() {
    cat << EOF
Usage: $0 <command> [options]

Commands:
    build           Build the Ansible container image
    deploy          Deploy Kubernetes VM on KVM host
    destroy         Destroy Kubernetes VM
    status          Check VM and cluster status
    run <playbook>  Run a specific playbook
    shell           Open a shell in the Ansible container

Options:
    --limit <host>      Limit execution to specific host
    -v, --verbose       Verbose output
    --check             Run in check mode
    --yes               Skip confirmation prompts (for destroy command)
    -h, --help          Show this help message

Examples:
    $0 build
    $0 deploy
    $0 deploy -v
    $0 destroy
    $0 destroy --yes
    $0 status
    $0 run deploy-k8s.yml --check
    $0 shell

Environment Variables:
    KVM_HOST_PASSWORD       SSH password for KVM host (if not using SSH keys)

Authentication:
    The script will use SSH key if available at ./ssh-key,
    otherwise it will use password authentication via KVM_HOST_PASSWORD.

EOF
}

# Main command processing
case "${1:-}" in
    build)
        build_image
        ;;

    deploy)
        build_image
        shift
        run_ansible "deploy-k8s.yml" "$@"
        ;;

    destroy)
        build_image
        shift
        # Check for --yes flag and filter it out from args
        extra_args=""
        filtered_args=()
        for arg in "$@"; do
            if [ "$arg" = "--yes" ] || [ "$arg" = "-y" ]; then
                extra_args="-e force_destroy=true"
            else
                filtered_args+=("$arg")
            fi
        done
        run_ansible "destroy-k8s.yml" $extra_args "${filtered_args[@]}"
        ;;

    status)
        build_image
        shift
        run_ansible "check-status.yml" "$@"
        ;;

    run)
        if [ -z "$2" ]; then
            print_error "Please specify a playbook to run"
            usage
            exit 1
        fi
        build_image
        shift
        playbook="$1"
        shift
        run_ansible "$playbook" "$@"
        ;;

    shell)
        build_image
        print_info "Opening shell in Ansible container..."
        podman run --rm -it \
            -v "$SCRIPT_DIR:/workspace:Z" \
            -v "$SCRIPT_DIR/../..:/repo:Z,ro" \
            --network host \
            --entrypoint /bin/bash \
            "$IMAGE_NAME"
        ;;

    -h|--help|help)
        usage
        ;;

    *)
        print_error "Unknown command: ${1:-}"
        echo ""
        usage
        exit 1
        ;;
esac
