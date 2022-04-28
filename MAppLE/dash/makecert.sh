#!/bin/sh
openssl req -x509 -newkey rsa:2048 -keyout privkey.pem -out cert.pem -days 356 -nodes
