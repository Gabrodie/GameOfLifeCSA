#!/bin/bash

KEY=~/.ssh/gol.pem

SERVER_IP=34.237.56.190
WORKER_IPS=(
    54.147.138.250
    18.232.42.255
    98.91.115.1
)

echo ""
echo "Starting WORKERS..."
for ip in "${WORKER_IPS[@]}"; do
    echo "→ $ip"
    ssh -i "$KEY" ec2-user@"$ip" \
        "nohup go run ~/worker.go > worker.log 2>&1 &"
done

echo "Starting SERVER on $SERVER_IP"
ssh -i "$KEY" ec2-user@"$SERVER_IP" \
    "nohup go run ~/server.go > server.log 2>&1 &"

echo ""
echo "Cluster started."
