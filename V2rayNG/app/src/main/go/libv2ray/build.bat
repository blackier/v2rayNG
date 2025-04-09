@echo off

@REM asserts
rmdir /s /q assets
mkdir assets
curl -o assets/geosite.dat -L https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat -x http://127.0.0.1:10809
curl -o assets/geoip.dat -L https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat -x http://127.0.0.1:10809

@REM libs
@REM go go env -w GOPROXY=https://goproxy.io,direct
@REM go install golang.org/x/mobile/cmd/gomobile@latest
pushd %~dp0..\..\..\..
rmdir /s /q libs
mkdir libs
popd
go mod tidy -v
gomobile init
gomobile bind -v -androidapi 24 -ldflags="-s -w" -target="android/arm64" -o ../../../../libs/libv2ray.aar ./
