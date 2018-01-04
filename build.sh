#!/bin/sh

VERSION=v0.2

HELP="Usage: 
	$0 [OPTION]

options:
	[none], install		build and install
	p, package		make tar package of all needed files
	c, clean		clean package
	r, run			start gogitserver
	d, debug		start gogitserver in debug mode
"

case "$1" in
	install|"")
		go install github.com/nananas/gogitserver
	;;
	clean|c)
		rm -r ./package
		rm ./gogitserver_*_linux_amd64.tar.gz
	;;
	package|p)
		mkdir -p ./package/static
		mkdir -p ./package/templates
		cp $GOPATH/bin/gogitserver ./package
		cp ./static/styling.css ./package/static
		cp ./templates/tree.html ./templates/index.html ./package/templates
		cp LICENSE README.md ./package
		cd package && tar cvf ../gogitserver_${VERSION}_linux_amd64.tar.gz *
	;;
	debug|d)
		$GOPATH/bin/gogitserver -d
	;;
	run|r)
		$GOPATH/bin/gogitserver
	;;
	*)
		echo "$HELP"
	;;
esac
