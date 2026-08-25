#!/bin/sh
# Stands in for the real `steampipe` CLI in tests: ignores the actual
# SQL and just prints a fixed JSON array, mimicking `steampipe query
# <sql> --output json`. Real usage points Client.Binary at the actual
# steampipe executable instead.
cat <<'EOF'
[
  {"name": "web-01", "moref": "vm-101", "num_cpu": 4, "memory_size": 8192, "power": "poweredOn", "guest_full_name": "Ubuntu Linux (64-bit)", "host_moref": "host-1"}
]
EOF
