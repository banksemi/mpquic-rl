#!/bin/bash
mkdir -p /certs
cd /certs
SUBJECT="/CN=10.0.0.20"
openssl rand -writerand /root/.rnd
if [ -f "privkey.pem" ]; then
    # If key exists, renew certificate only (using existing key)
    openssl req -x509 -key privkey.pem -out cert.pem -days 365 -nodes -subj "$SUBJECT"
else
    # If key doesn't exist, create new key and certificate
    openssl req -x509 -newkey rsa:2048 -keyout privkey.pem -out cert.pem -days 365 -nodes -subj "$SUBJECT"
fi
