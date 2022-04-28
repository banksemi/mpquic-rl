#!/bin/bash

cd ../MAppLE/caddy/caddy

# Current path: /MAppLE/caddy/caddy
echo "Build the caddy server..."
go build
if [ $? -ne 0 ]; then { echo "Failed, aborting." ; exit 1; } fi

echo "Copy the build file... (to /MAppLE/dash/caddy)"
cp ./caddy ../../dash/caddy
if [ $? -ne 0 ]; then { echo "Failed, aborting." ; exit 1; } fi

echo "Delete an existing built file..."
rm ./caddy
if [ $? -ne 0 ]; then { echo "Failed, aborting." ; exit 1; } fi


# Current path: /MAppLE/caddy/caddy
cd ../../proxy_module
if [ $? -ne 0 ]; then { echo "./proxy_module directory does not exist." ; exit 1; } fi
# Current path: /MAppLE/proxy_module

echo "Build the dash proxy module..."
go build -o proxy_module.so -buildmode=c-shared proxy_module.go
if [ $? -ne 0 ]; then { echo "Failed, aborting." ; exit 1; } fi

echo "Copy the build file... (to /MAppLE/astream/proxy_module.so)"
cp ./proxy_module.so ../astream/proxy_module.so
if [ $? -ne 0 ]; then { echo "Failed, aborting." ; exit 1; } fi

