#!/bin/bash

echo "Stop an existing container..."
sudo docker stop mininet > /dev/null 2>&1

echo "Remove an existing container..."
sudo docker rm mininet > /dev/null 2>&1

echo "Build docker image..."
sudo docker build -t mininet ../docker

cd ../
# Current Directory: /
echo "Create mininet container and start it"
# $PWD/docker/output LinUCB parameter hardcoded in Peekaboo source code /App/output/lin (in.scheduler.go)
sudo docker run --privileged \
	--cap-add=ALL \
	-v /lib/modules:/lib/modules \
	-v $PWD/docker:/docker \
	-v $PWD/docker/output:/App/output \
	-p 8888:8888 \
	--add-host quic.clemente.io:10.0.0.20 \
	--name mininet \
	mininet
ip=$(hostname -I | awk '{print $1}')
echo "Check your jupyter link: http://$ip:8888/"

