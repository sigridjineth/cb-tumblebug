#!/bin/bash

source ../conf.env

curl -sX GET http://localhost:1024/spider/vm/VM-01 -H 'Content-Type: application/json' -d '{ "ConnectionName": "'${CONN_CONFIG}'"}' | json_pp