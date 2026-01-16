#!/bin/bash
#
# AGS CLI End-to-End Test Script
# Tests: apikey, tool, instance lifecycle with both cloud and e2b backends
#
# Prerequisites:
#   - ags CLI built and in PATH (or run from repo root)
#   - Cloud backend configured (~/.ags/config.toml or env vars)
#
# Usage:
#   ./examples/e2e_test.sh [options]
#
# Options:
#   -r, --region REGION    Set region (default: ap-guangzhou)
#   -i, --internal         Use internal endpoints
#   -d, --domain DOMAIN    Set E2B domain (default: tencentags.com)
#   -h, --help             Show this help message
#

# Don't use set -e as we want to continue on errors and report them

# Default values
REGION="ap-guangzhou"
INTERNAL=""
E2B_DOMAIN="tencentags.com"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -r|--region)
            REGION="$2"
            shift 2
            ;;
        -i|--internal)
            INTERNAL="--cloud-internal"
            shift
            ;;
        -d|--domain)
            E2B_DOMAIN="$2"
            shift 2
            ;;
        -h|--help)
            echo "AGS CLI End-to-End Test Script"
            echo ""
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  -r, --region REGION    Set region (default: ap-guangzhou)"
            echo "  -i, --internal         Use internal endpoints"
            echo "  -d, --domain DOMAIN    Set E2B domain (default: tencentags.com)"
            echo "  -h, --help             Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use -h or --help for usage information"
            exit 1
            ;;
    esac
done

# Build common flags
# Note: exec/file commands use SDK which reads cloud-region, so we set both regions for E2B backend
CLOUD_FLAGS="--backend cloud --cloud-region $REGION $INTERNAL"
E2B_FLAGS="--backend e2b --e2b-region $REGION --cloud-region $REGION --e2b-domain $E2B_DOMAIN"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test state
CREATED_APIKEY_ID=""
CREATED_APIKEY_VALUE=""
CREATED_TOOL_ID=""
CREATED_INSTANCE_ID=""
ERRORS=0

# Helper functions
log_step() {
    printf "\n${BLUE}=== Step $1: $2 ===${NC}\n"
}

log_success() {
    printf "${GREEN}✓ $1${NC}\n"
}

log_error() {
    printf "${RED}✗ $1${NC}\n"
    ERRORS=$((ERRORS + 1))
}

log_info() {
    printf "${YELLOW}  $1${NC}\n"
}

# Wait with countdown
wait_with_countdown() {
    local seconds=$1
    local message=${2:-"Waiting"}
    for i in $(seq $seconds -1 1); do
        printf "\r${YELLOW}  %s... %d seconds remaining${NC}  " "$message" "$i"
        sleep 1
    done
    printf "\r${YELLOW}  %s... done                    ${NC}\n" "$message"
}

# Check if ags is available
if command -v ags >/dev/null 2>&1; then
    AGS="ags"
elif [ -f "./ags" ]; then
    AGS="./ags"
else
    printf "${RED}Error: ags CLI not found. Please build it first: make build${NC}\n"
    exit 1
fi

printf "${BLUE}Using AGS CLI: $AGS${NC}\n"
printf "${BLUE}Region: $REGION${NC}\n"
printf "${BLUE}Internal: ${INTERNAL:-no}${NC}\n"
printf "${BLUE}E2B Domain: $E2B_DOMAIN${NC}\n"
printf "${BLUE}Starting E2E tests...${NC}\n"

# ============================================
# Step 1: List API Keys (initial state)
# ============================================
log_step 1 "List API Keys (initial state)"

# JSON output is {items: [...], pagination: {...}}
INITIAL_APIKEY_LIST=$($AGS $CLOUD_FLAGS ak ls -o json 2>/dev/null || echo '{"items":[]}')
INITIAL_APIKEY_COUNT=$(echo "$INITIAL_APIKEY_LIST" | jq '.items | length' 2>/dev/null || echo "0")
log_info "Initial API key count: $INITIAL_APIKEY_COUNT"
log_success "Listed API keys"

# ============================================
# Step 2: Create a new API Key
# ============================================
log_step 2 "Create a new API Key"

TEST_APIKEY_NAME="test-key-$(date +%s)"

# Create returns text format by default, capture the output
CREATE_OUTPUT=$($AGS $CLOUD_FLAGS ak create -n "$TEST_APIKEY_NAME" 2>&1) || true
CREATE_EXIT=$?

if [ $CREATE_EXIT -eq 0 ] && [ -n "$CREATE_OUTPUT" ]; then
    # Parse from text output: "API key created: ark-xxx"
    # And the table output contains KeyID, Name, APIKey
    CREATED_APIKEY_ID=$(echo "$CREATE_OUTPUT" | grep -oE 'ark-[a-z0-9]+' | head -1)
    # Try to get the API key value from the output (format: ark_xxx-xxx)
    CREATED_APIKEY_VALUE=$(echo "$CREATE_OUTPUT" | grep -oE 'ark_[A-Za-z0-9_-]+' | head -1)
    
    if [ -n "$CREATED_APIKEY_ID" ]; then
        log_success "Created API key: $CREATED_APIKEY_ID"
        log_info "API key name: $TEST_APIKEY_NAME"
        if [ -n "$CREATED_APIKEY_VALUE" ]; then
            log_info "API key value: ${CREATED_APIKEY_VALUE:0:20}..."
        fi
    else
        log_error "Failed to parse API key ID from response"
        printf "Output: %s\n" "$CREATE_OUTPUT"
    fi
else
    log_error "Failed to create API key (exit code: $CREATE_EXIT)"
    printf "Output: %s\n" "$CREATE_OUTPUT"
fi

# ============================================
# Step 3: List API Keys (verify new key)
# ============================================
log_step 3 "List API Keys (verify new key exists)"

NEW_APIKEY_LIST=$($AGS $CLOUD_FLAGS ak ls -o json 2>/dev/null || echo '{"items":[]}')
NEW_APIKEY_COUNT=$(echo "$NEW_APIKEY_LIST" | jq '.items | length' 2>/dev/null || echo "0")
log_info "New API key count: $NEW_APIKEY_COUNT"

if [ "$NEW_APIKEY_COUNT" -gt "$INITIAL_APIKEY_COUNT" ]; then
    log_success "API key count increased"
elif [ -n "$CREATED_APIKEY_ID" ]; then
    log_error "API key count did not increase (expected > $INITIAL_APIKEY_COUNT, got $NEW_APIKEY_COUNT)"
fi

# Check if our key exists by ID
if [ -n "$CREATED_APIKEY_ID" ]; then
    KEY_EXISTS=$(echo "$NEW_APIKEY_LIST" | jq -r ".items[] | select(.[\"KEY ID\"] == \"$CREATED_APIKEY_ID\") | .[\"KEY ID\"]" 2>/dev/null || echo "")
    if [ -n "$KEY_EXISTS" ]; then
        log_success "Found newly created API key in list"
    else
        log_error "Could not find newly created API key in list"
    fi
fi

# ============================================
# Step 4: List Tools (initial state)
# ============================================
log_step 4 "List Tools (initial state)"

# JSON output is {items: [...], pagination: {...}}
INITIAL_TOOL_LIST=$($AGS $CLOUD_FLAGS t ls -o json 2>/dev/null || echo '{"items":[]}')
INITIAL_TOOL_COUNT=$(echo "$INITIAL_TOOL_LIST" | jq '.items | length' 2>/dev/null || echo "0")
log_info "Initial tool count: $INITIAL_TOOL_COUNT"
log_success "Listed tools"

# ============================================
# Step 5: Create a new Tool
# ============================================
log_step 5 "Create a new Tool"

TEST_TOOL_NAME="test-tool-$(date +%s)"

# Create returns text format
TOOL_CREATE_OUTPUT=$($AGS $CLOUD_FLAGS t create -n "$TEST_TOOL_NAME" -t code-interpreter -d "E2E test tool" 2>&1) || true
TOOL_CREATE_EXIT=$?

if [ $TOOL_CREATE_EXIT -eq 0 ] && [ -n "$TOOL_CREATE_OUTPUT" ]; then
    # Parse from text output: "Tool created: sdt-xxx"
    CREATED_TOOL_ID=$(echo "$TOOL_CREATE_OUTPUT" | grep -oE 'sdt-[a-z0-9]+' | head -1)
    
    if [ -n "$CREATED_TOOL_ID" ]; then
        log_success "Created tool: $CREATED_TOOL_ID"
        log_info "Tool name: $TEST_TOOL_NAME"
    else
        log_error "Failed to parse tool ID from response"
        printf "Output: %s\n" "$TOOL_CREATE_OUTPUT"
    fi
else
    log_error "Failed to create tool (exit code: $TOOL_CREATE_EXIT)"
    printf "Output: %s\n" "$TOOL_CREATE_OUTPUT"
fi

# ============================================
# Step 6: List Tools (verify new tool)
# ============================================
log_step 6 "List Tools (verify new tool exists)"

# Wait longer for tool to be available (tools need time to provision)
wait_with_countdown 5 "Waiting for tool to be available"

NEW_TOOL_LIST=$($AGS $CLOUD_FLAGS t ls -o json 2>/dev/null || echo '{"items":[]}')
NEW_TOOL_COUNT=$(echo "$NEW_TOOL_LIST" | jq '.items | length' 2>/dev/null || echo "0")
log_info "New tool count: $NEW_TOOL_COUNT"

if [ "$NEW_TOOL_COUNT" -gt "$INITIAL_TOOL_COUNT" ]; then
    log_success "Tool count increased"
elif [ -n "$CREATED_TOOL_ID" ]; then
    log_info "Tool count unchanged (may be expected if tool is still creating)"
fi

# Check if our tool exists by ID
if [ -n "$CREATED_TOOL_ID" ]; then
    TOOL_EXISTS=$(echo "$NEW_TOOL_LIST" | jq -r ".items[] | select(.ID == \"$CREATED_TOOL_ID\") | .ID" 2>/dev/null || echo "")
    if [ -n "$TOOL_EXISTS" ]; then
        log_success "Found newly created tool in list"
    else
        log_error "Could not find newly created tool in list"
    fi
fi

# ============================================
# Step 7: List Instances (initial state)
# ============================================
log_step 7 "List Instances (initial state)"

# Check if we have a valid API key to use for E2B
if [ -z "$CREATED_APIKEY_VALUE" ]; then
    log_error "No API key value available, cannot proceed with E2B tests"
    INITIAL_INSTANCE_COUNT=0
else
    # JSON output is {items: [...], pagination: {...}}
    INITIAL_INSTANCE_LIST=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" i ls -o json 2>/dev/null || echo '{"items":[]}')
    INITIAL_INSTANCE_COUNT=$(echo "$INITIAL_INSTANCE_LIST" | jq '.items | length' 2>/dev/null || echo "0")
    log_info "Initial instance count: $INITIAL_INSTANCE_COUNT"
    log_success "Listed instances"
fi

# ============================================
# Step 8: Create and use Instance with E2B backend
# ============================================
log_step 8 "Create and use Instance (E2B backend)"

if [ -z "$CREATED_APIKEY_VALUE" ]; then
    log_error "Skipping E2B tests - no API key available"
else
    # Use default tool for instance tests (newly created tools need time to be ready)
    TOOL_TO_USE="code-interpreter-v1"
    log_info "Using tool: $TOOL_TO_USE"
    log_info "Using API key: ${CREATED_APIKEY_VALUE:0:20}..."

    # Create instance and run code
    log_info "Creating instance and executing code..."

    RUN_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" r -t "$TOOL_TO_USE" -c "print('Hello from E2E test!')" --time 2>&1)
    RUN_EXIT=$?

    if [ $RUN_EXIT -eq 0 ]; then
        log_success "Code execution completed"
        printf "%s\n" "$RUN_OUTPUT" | head -5
    else
        log_error "Code execution failed (exit code: $RUN_EXIT)"
        printf "%s\n" "$RUN_OUTPUT"
    fi

    # Also test with streaming
    log_info "Testing streaming output..."
    STREAM_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" r -t "$TOOL_TO_USE" -s -c "for i in range(3): print(f'Count: {i}')" 2>&1)
    STREAM_EXIT=$?

    if [ $STREAM_EXIT -eq 0 ]; then
        log_success "Streaming execution completed"
        printf "%s\n" "$STREAM_OUTPUT"
    else
        log_error "Streaming execution failed (exit code: $STREAM_EXIT)"
        printf "%s\n" "$STREAM_OUTPUT"
    fi

    # Create a persistent instance to verify in list
    log_info "Creating persistent instance..."
    INSTANCE_CREATE_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" i c -t "$TOOL_TO_USE" 2>&1)
    INSTANCE_CREATE_EXIT=$?

    if [ $INSTANCE_CREATE_EXIT -eq 0 ]; then
        # Parse instance ID from output - format is 32-char hex string
        CREATED_INSTANCE_ID=$(echo "$INSTANCE_CREATE_OUTPUT" | grep -oE '[a-f0-9]{32}' | head -1)
        if [ -n "$CREATED_INSTANCE_ID" ]; then
            log_success "Created instance: $CREATED_INSTANCE_ID"
        else
            log_info "Instance created but could not parse ID"
            printf "%s\n" "$INSTANCE_CREATE_OUTPUT"
        fi
    else
        log_error "Failed to create instance (exit code: $INSTANCE_CREATE_EXIT)"
        printf "%s\n" "$INSTANCE_CREATE_OUTPUT"
    fi
fi

# ============================================
# Step 9: List Instances (verify new instance)
# ============================================
log_step 9 "List Instances (verify new instance)"

if [ -n "$CREATED_APIKEY_VALUE" ]; then
    wait_with_countdown 5 "Waiting for instance to be ready"

    NEW_INSTANCE_LIST=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" i ls -o json 2>/dev/null || echo '{"items":[]}')
    NEW_INSTANCE_COUNT=$(echo "$NEW_INSTANCE_LIST" | jq '.items | length' 2>/dev/null || echo "0")
    log_info "New instance count: $NEW_INSTANCE_COUNT"

    if [ -n "$CREATED_INSTANCE_ID" ]; then
        # Check if instance exists in list
        INSTANCE_EXISTS=$(echo "$NEW_INSTANCE_LIST" | jq -r ".items[] | select(.ID == \"$CREATED_INSTANCE_ID\") | .ID" 2>/dev/null || echo "")
        if [ -n "$INSTANCE_EXISTS" ]; then
            log_success "Found newly created instance in list"
        else
            log_info "Instance may have already terminated or ID format differs"
        fi
    fi
else
    log_info "Skipping - no API key available"
fi

# ============================================
# Step 10: Test exec command (shell execution)
# ============================================
log_step 10 "Test exec command (shell execution)"

if [ -n "$CREATED_INSTANCE_ID" ] && [ -n "$CREATED_APIKEY_VALUE" ]; then
    # Test basic exec
    log_info "Testing exec command..."
    EXEC_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" x --instance "$CREATED_INSTANCE_ID" "echo 'Hello from exec'" 2>&1)
    EXEC_EXIT=$?

    if [ $EXEC_EXIT -eq 0 ]; then
        log_success "exec command completed"
        printf "%s\n" "$EXEC_OUTPUT"
    else
        log_error "exec command failed (exit code: $EXEC_EXIT)"
        printf "%s\n" "$EXEC_OUTPUT"
    fi

    # Test exec with cwd
    log_info "Testing exec with working directory..."
    EXEC_CWD_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" x --instance "$CREATED_INSTANCE_ID" --cwd /home/user "pwd" 2>&1)
    EXEC_CWD_EXIT=$?

    if [ $EXEC_CWD_EXIT -eq 0 ]; then
        log_success "exec with cwd completed"
        printf "%s\n" "$EXEC_CWD_OUTPUT"
    else
        log_error "exec with cwd failed (exit code: $EXEC_CWD_EXIT)"
        printf "%s\n" "$EXEC_CWD_OUTPUT"
    fi

    # Test exec with env
    log_info "Testing exec with environment variable..."
    EXEC_ENV_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" x --instance "$CREATED_INSTANCE_ID" --env "TEST_VAR=hello_e2e" 'echo $TEST_VAR' 2>&1)
    EXEC_ENV_EXIT=$?

    if [ $EXEC_ENV_EXIT -eq 0 ]; then
        log_success "exec with env completed"
        printf "%s\n" "$EXEC_ENV_OUTPUT"
    else
        log_error "exec with env failed (exit code: $EXEC_ENV_EXIT)"
        printf "%s\n" "$EXEC_ENV_OUTPUT"
    fi

    # Test exec ps
    log_info "Testing exec ps (list processes)..."
    EXEC_PS_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" x ps --instance "$CREATED_INSTANCE_ID" 2>&1)
    EXEC_PS_EXIT=$?

    if [ $EXEC_PS_EXIT -eq 0 ]; then
        log_success "exec ps completed"
        printf "%s\n" "$EXEC_PS_OUTPUT" | head -10
    else
        log_error "exec ps failed (exit code: $EXEC_PS_EXIT)"
        printf "%s\n" "$EXEC_PS_OUTPUT"
    fi
else
    log_info "Skipping exec tests - no instance available"
fi

# ============================================
# Step 11: Test file command (file operations)
# ============================================
log_step 11 "Test file command (file operations)"

if [ -n "$CREATED_INSTANCE_ID" ] && [ -n "$CREATED_APIKEY_VALUE" ]; then
    # Test file list
    log_info "Testing file list..."
    FILE_LIST_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" f ls /home/user --instance "$CREATED_INSTANCE_ID" 2>&1)
    FILE_LIST_EXIT=$?

    if [ $FILE_LIST_EXIT -eq 0 ]; then
        log_success "file list completed"
        printf "%s\n" "$FILE_LIST_OUTPUT" | head -10
    else
        log_error "file list failed (exit code: $FILE_LIST_EXIT)"
        printf "%s\n" "$FILE_LIST_OUTPUT"
    fi

    # Test file mkdir
    log_info "Testing file mkdir..."
    FILE_MKDIR_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" f mkdir /home/user/e2e_test_dir --instance "$CREATED_INSTANCE_ID" 2>&1)
    FILE_MKDIR_EXIT=$?

    if [ $FILE_MKDIR_EXIT -eq 0 ]; then
        log_success "file mkdir completed"
    else
        log_error "file mkdir failed (exit code: $FILE_MKDIR_EXIT)"
        printf "%s\n" "$FILE_MKDIR_OUTPUT"
    fi

    # Test file upload (create a temp file first)
    log_info "Testing file upload..."
    TEMP_FILE=$(mktemp)
    echo "E2E test content - $(date)" > "$TEMP_FILE"
    FILE_UPLOAD_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" f up "$TEMP_FILE" /home/user/e2e_test_dir/test.txt --instance "$CREATED_INSTANCE_ID" 2>&1)
    FILE_UPLOAD_EXIT=$?
    rm -f "$TEMP_FILE"

    if [ $FILE_UPLOAD_EXIT -eq 0 ]; then
        log_success "file upload completed"
    else
        log_error "file upload failed (exit code: $FILE_UPLOAD_EXIT)"
        printf "%s\n" "$FILE_UPLOAD_OUTPUT"
    fi

    # Test file cat
    log_info "Testing file cat..."
    FILE_CAT_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" f cat /home/user/e2e_test_dir/test.txt --instance "$CREATED_INSTANCE_ID" 2>&1)
    FILE_CAT_EXIT=$?

    if [ $FILE_CAT_EXIT -eq 0 ]; then
        log_success "file cat completed"
        printf "%s\n" "$FILE_CAT_OUTPUT"
    else
        log_error "file cat failed (exit code: $FILE_CAT_EXIT)"
        printf "%s\n" "$FILE_CAT_OUTPUT"
    fi

    # Test file stat
    log_info "Testing file stat..."
    FILE_STAT_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" f stat /home/user/e2e_test_dir/test.txt --instance "$CREATED_INSTANCE_ID" 2>&1)
    FILE_STAT_EXIT=$?

    if [ $FILE_STAT_EXIT -eq 0 ]; then
        log_success "file stat completed"
        printf "%s\n" "$FILE_STAT_OUTPUT"
    else
        log_error "file stat failed (exit code: $FILE_STAT_EXIT)"
        printf "%s\n" "$FILE_STAT_OUTPUT"
    fi

    # Test file download
    log_info "Testing file download..."
    DOWNLOAD_FILE=$(mktemp)
    FILE_DOWNLOAD_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" f down /home/user/e2e_test_dir/test.txt "$DOWNLOAD_FILE" --instance "$CREATED_INSTANCE_ID" 2>&1)
    FILE_DOWNLOAD_EXIT=$?

    if [ $FILE_DOWNLOAD_EXIT -eq 0 ]; then
        log_success "file download completed"
        log_info "Downloaded content: $(cat "$DOWNLOAD_FILE")"
    else
        log_error "file download failed (exit code: $FILE_DOWNLOAD_EXIT)"
        printf "%s\n" "$FILE_DOWNLOAD_OUTPUT"
    fi
    rm -f "$DOWNLOAD_FILE"

    # Test file remove
    log_info "Testing file remove..."
    FILE_RM_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" f rm /home/user/e2e_test_dir --instance "$CREATED_INSTANCE_ID" 2>&1)
    FILE_RM_EXIT=$?

    if [ $FILE_RM_EXIT -eq 0 ]; then
        log_success "file remove completed"
    else
        log_error "file remove failed (exit code: $FILE_RM_EXIT)"
        printf "%s\n" "$FILE_RM_OUTPUT"
    fi
else
    log_info "Skipping file tests - no instance available"
fi

# ============================================
# Step 12: Delete Instance
# ============================================
log_step 12 "Delete Instance"

if [ -n "$CREATED_INSTANCE_ID" ] && [ -n "$CREATED_APIKEY_VALUE" ]; then
    DELETE_INSTANCE_OUTPUT=$($AGS $E2B_FLAGS --e2b-api-key "$CREATED_APIKEY_VALUE" i rm "$CREATED_INSTANCE_ID" 2>&1)
    DELETE_INSTANCE_EXIT=$?
    
    if [ $DELETE_INSTANCE_EXIT -eq 0 ]; then
        log_success "Deleted instance: $CREATED_INSTANCE_ID"
    else
        log_error "Failed to delete instance (exit code: $DELETE_INSTANCE_EXIT)"
        printf "%s\n" "$DELETE_INSTANCE_OUTPUT"
    fi
else
    log_info "No instance to delete"
fi

# ============================================
# Step 13: Delete Tool
# ============================================
wait_with_countdown 5 "Waiting for instance to terminate before deleting tool"
log_step 13 "Delete Tool"

if [ -n "$CREATED_TOOL_ID" ]; then
    DELETE_TOOL_OUTPUT=$($AGS $CLOUD_FLAGS t rm "$CREATED_TOOL_ID" 2>&1)
    DELETE_TOOL_EXIT=$?
    
    if [ $DELETE_TOOL_EXIT -eq 0 ]; then
        log_success "Deleted tool: $CREATED_TOOL_ID"
    else
        log_error "Failed to delete tool (exit code: $DELETE_TOOL_EXIT)"
        printf "%s\n" "$DELETE_TOOL_OUTPUT"
    fi
    
    # Verify deletion
    TOOL_STILL_EXISTS=$($AGS $CLOUD_FLAGS t ls --id "$CREATED_TOOL_ID" -o json 2>/dev/null | jq '.items | length' 2>/dev/null || echo "0")
    if [ "$TOOL_STILL_EXISTS" -eq 0 ]; then
        log_success "Verified tool is deleted"
    else
        log_info "Tool may still be in DELETING state"
    fi
else
    log_info "No tool to delete"
fi

# ============================================
# Step 14: Delete API Key
# ============================================
log_step 14 "Delete API Key"

if [ -n "$CREATED_APIKEY_ID" ]; then
    DELETE_APIKEY_OUTPUT=$($AGS $CLOUD_FLAGS ak rm "$CREATED_APIKEY_ID" 2>&1)
    DELETE_APIKEY_EXIT=$?
    
    if [ $DELETE_APIKEY_EXIT -eq 0 ]; then
        log_success "Deleted API key: $CREATED_APIKEY_ID"
    else
        log_error "Failed to delete API key (exit code: $DELETE_APIKEY_EXIT)"
        printf "%s\n" "$DELETE_APIKEY_OUTPUT"
    fi
    
    # Verify deletion
    wait_with_countdown 1 "Verifying API key deletion"
    FINAL_APIKEY_LIST=$($AGS $CLOUD_FLAGS ak ls -o json 2>/dev/null || echo '{"items":[]}')
    KEY_STILL_EXISTS=$(echo "$FINAL_APIKEY_LIST" | jq -r ".items[] | select(.[\"KEY ID\"] == \"$CREATED_APIKEY_ID\") | .[\"KEY ID\"]" 2>/dev/null || echo "")
    if [ -z "$KEY_STILL_EXISTS" ]; then
        log_success "Verified API key is deleted"
    else
        log_error "API key still exists after deletion"
    fi
else
    log_info "No API key to delete"
fi

# ============================================
# Summary
# ============================================
printf "\n${BLUE}=== Test Summary ===${NC}\n"
printf "Created API Key: ${CREATED_APIKEY_ID:-none}\n"
printf "Created Tool: ${CREATED_TOOL_ID:-none}\n"
printf "Created Instance: ${CREATED_INSTANCE_ID:-none}\n"
printf "Total steps: 14\n"
printf "Errors: $ERRORS\n"

if [ $ERRORS -eq 0 ]; then
    printf "\n${GREEN}All tests passed!${NC}\n"
    exit 0
else
    printf "\n${RED}Tests completed with $ERRORS error(s)${NC}\n"
    exit 1
fi
