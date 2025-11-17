#!/bin/bash

KEY="$HOME/.ssh/gol.pem"

SERVER_IP=34.237.56.190
WORKER_IPS=(
    54.147.138.250
    18.232.42.255
    98.91.115.1
)

PORT=7000

echo "Stopping SERVER on $SERVER_IP"
ssh -i "$KEY" ec2-user@"$SERVER_IP" "
    PID=\$(lsof -ti :$PORT);
    if [ -n \"\$PID\" ]; then
        echo \"Killing server PID \$PID\";
        kill \$PID;
    else
        echo \"No process on port $PORT\";
    fi
"

echo ""
echo "Stopping WORKERS..."
for ip in "${WORKER_IPS[@]}"; do
    echo "→ $ip"
    ssh -i "$KEY" ec2-user@"$ip" "
        PID=\$(lsof -ti :$PORT);
        if [ -n \"\$PID\" ]; then
            echo \"Killing worker PID \$PID\";
            kill \$PID;
        else
            echo \"No process on port $PORT\";
        fi
    "
done

echo ""
echo "Cluster stopped."
