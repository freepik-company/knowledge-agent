#!/bin/sh
set -e

echo "🚀 Knowledge Agent - Starting..."
echo ""

# ============================================================================
# NPM Package Auto-Detection and Installation
# ============================================================================
# This script automatically extracts npm packages from MCP server configurations
# in config.yaml where command.path is "npx"
#
# Two installation modes:
# 1. Auto-detection from config.yaml (recommended)
# 2. Manual via MCP_NPM_PACKAGES env var (fallback)
# ============================================================================

extract_npm_packages_from_config() {
    local config_file="$1"
    local packages=""

    if [ ! -f "$config_file" ]; then
        return 0
    fi

    # Extract npm packages from npx commands in config.yaml
    # Pattern: path: "npx" followed by args with package name (after "-y" flag)
    # Example:
    #   path: "npx"
    #   args:
    #     - "-y"
    #     - "@modelcontextprotocol/server-filesystem"

    # Parse YAML to find npx commands and extract package names
    # This is a simple parser - assumes standard YAML formatting
    in_mcp_section=0
    in_command_section=0
    in_args_section=0
    skip_next_arg=0

    while IFS= read -r line; do
        # Check if we're in MCP section
        if echo "$line" | grep -q "^mcp:"; then
            in_mcp_section=1
            continue
        fi

        # Exit MCP section if we hit another top-level key
        if [ $in_mcp_section -eq 1 ] && echo "$line" | grep -q "^[a-z_]*:"; then
            in_mcp_section=0
        fi

        if [ $in_mcp_section -eq 1 ]; then
            # Check for command section
            if echo "$line" | grep -q "command:"; then
                in_command_section=1
                in_args_section=0
                continue
            fi

            # Check if command path is npx
            if [ $in_command_section -eq 1 ] && echo "$line" | grep -q 'path:.*"npx"'; then
                in_args_section=1
                skip_next_arg=0
                continue
            fi

            # Extract package from args
            if [ $in_args_section -eq 1 ]; then
                # Check if line starts args array
                if echo "$line" | grep -q "args:"; then
                    continue
                fi

                # Check if we're still in args (starts with dash and spaces)
                if echo "$line" | grep -q "^[[:space:]]*-[[:space:]]"; then
                    # Extract the value (remove leading spaces, dash, quotes)
                    arg=$(echo "$line" | sed 's/^[[:space:]]*-[[:space:]]*//; s/"//g; s/'\''//g')

                    # Skip "-y" flag
                    if [ "$arg" = "-y" ]; then
                        skip_next_arg=0
                        continue
                    fi

                    # This should be the package name (starts with @ or alphanumeric)
                    if echo "$arg" | grep -q "^[@a-zA-Z0-9]"; then
                        if [ -n "$packages" ]; then
                            packages="$packages $arg"
                        else
                            packages="$arg"
                        fi
                        in_args_section=0
                        in_command_section=0
                    fi
                else
                    # End of args section
                    in_args_section=0
                    in_command_section=0
                fi
            fi
        fi
    done < "$config_file"

    echo "$packages"
}

# Try to extract packages from config.yaml
NPM_PACKAGES_AUTO=""
if [ -n "$CONFIG_PATH" ] && [ -f "$CONFIG_PATH" ]; then
    echo "🔍 Scanning config.yaml for npm packages..."
    NPM_PACKAGES_AUTO=$(extract_npm_packages_from_config "$CONFIG_PATH")

    if [ -n "$NPM_PACKAGES_AUTO" ]; then
        echo "   Found packages: $NPM_PACKAGES_AUTO"
    else
        echo "   No npx-based MCP servers found in config"
    fi
fi

# Combine auto-detected and manual packages
PACKAGES_TO_INSTALL=""
if [ -n "$NPM_PACKAGES_AUTO" ]; then
    PACKAGES_TO_INSTALL="$NPM_PACKAGES_AUTO"
fi
if [ -n "$MCP_NPM_PACKAGES" ]; then
    if [ -n "$PACKAGES_TO_INSTALL" ]; then
        PACKAGES_TO_INSTALL="$PACKAGES_TO_INSTALL $MCP_NPM_PACKAGES"
    else
        PACKAGES_TO_INSTALL="$MCP_NPM_PACKAGES"
    fi
    echo "   Additional packages from env: $MCP_NPM_PACKAGES"
fi

# Install packages if any found
if [ -n "$PACKAGES_TO_INSTALL" ]; then
    echo "📦 Installing npm packages..."
    echo "   Packages: $PACKAGES_TO_INSTALL"

    if npm install -g $PACKAGES_TO_INSTALL 2>&1 | grep -v "npm WARN"; then
        echo "   ✅ Packages installed successfully"
    else
        echo "   ⚠️  Package installation failed (continuing anyway)"
    fi
    echo ""
else
    echo "ℹ️  No npm packages to install"
    echo ""
fi

# ============================================================================
# Start Application
# ============================================================================

echo "🎯 Starting Knowledge Agent..."
echo "   Mode: ${MODE:-all}"
if [ -f "$CONFIG_PATH" ]; then
    echo "   Config: $CONFIG_PATH"
fi
echo ""

# Execute the main application
if [ -n "$CONFIG_PATH" ] && [ -f "$CONFIG_PATH" ]; then
    exec /bin/knowledge-agent --config "$CONFIG_PATH" --mode "${MODE:-all}"
else
    exec /bin/knowledge-agent --mode "${MODE:-all}"
fi
