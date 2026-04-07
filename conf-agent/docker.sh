#!/bin/bash

VERION=$1

if [ "$1" == "" ]
then
	VERSION=`git tag |tail -1`
fi

PLATFORM="amd64"
MACHINE=`uname -m`

if [ $MACHINE == "aarch64" ]
then
	PLATFORM="arm64"
fi

REPO=`git remote -v |head -1 |cut -d $':' -f 2 |cut -d '.' -f 1`
TAG="registry.gitlab.com/$REPO:$PLATFORM.$VERSION"

echo "Building $TAG ........................................."

go build &&
docker build -t "$TAG" --build-arg ARCH="$PLATFORM" . &&
docker push "$TAG" &&

echo "$TAG completed"
