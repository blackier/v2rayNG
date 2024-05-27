#!/bin/bash

# asserts
rmdir assets
mkdir assets
curl -o assets/geosite.dat -L https://github.com/v2ray/domain-list-community/raw/release/dlc.dat
curl -o assets/geoip.dat -L https://github.com/v2ray/geoip/raw/release/geoip.dat

# libs
mkdir ../../../../libs
gomobile init
gomobile bind -v -o ../../../../libs/libv2ray.aar -androidapi 21 -ldflags "-s -w -buildid=" .
