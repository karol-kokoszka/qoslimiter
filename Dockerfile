FROM golang:1.15.8-alpine3.13 AS build

RUN apk add --update build-base git curl bash
