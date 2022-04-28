#!/bin/bash

echo "Stop an existing container..."
sudo docker stop mininet > /dev/null 2>&1

echo "Remove an existing container..."
sudo docker rm mininet > /dev/null 2>&1

echo "Build docker image..."
sudo docker build -t mininet ../docker

echo "Create mininet container and start it"
sudo docker run --privileged \
	--cap-add=ALL \
	-v /lib/modules:/lib/modules \
	-p 8888:8888 \
	--add-host quic.clemente.io:10.0.0.20 \
	--name mininet \
	mininet
ip=$(hostname -I | awk '{print $1}')
echo "Check your jupyter link: http://$ip:8888/"

