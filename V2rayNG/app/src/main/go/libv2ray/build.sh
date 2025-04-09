#!/bin/bash

# asserts
rmdir assets
mkdir assets
curl -o assets/geosite.dat -L https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat -x http://127.0.0.1:10809
curl -o assets/geoip.dat -L https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat -x http://127.0.0.1:10809

# libs
# go go env -w GOPROXY=https://goproxy.io,direct
# go install golang.org/x/mobile/cmd/gomobile@latest
mkdir ../../../../libs
# go mod tidy -v
gomobile init
gomobile bind -v -o ../../../../libs/libv2ray.aar -androidapi 21 -ldflags "-s -w -buildid=" .
