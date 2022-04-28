#!/bin/bash

# Check old directory
DIR="/usr/local/go"
if [ -d "$DIR" ]; then
  # Take action if $DIR exists. #
  echo "Remove old version"
  rm "$DIR" -r
fi

# Download and setup
GOLANG_VERSION=1.16.5
curl -LO https://dl.google.com/go/go${GOLANG_VERSION}.linux-amd64.tar.gz
tar -C /usr/local -xzf go${GOLANG_VERSION}.linux-amd64.tar.gz

echo 'export GOROOT=$DIR' >> ~/.bashrc
