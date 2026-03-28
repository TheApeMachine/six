#!/bin/bash
go run main.go viz --listen &
VIZ_PID=$!
sleep 2
go test -v ./experiment/task -run TestPipeline/Text_Classification -timeout 2m
kill $VIZ_PID
