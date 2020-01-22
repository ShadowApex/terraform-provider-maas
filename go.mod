module github.com/seanhoughton/terraform-provider-maas

go 1.13

require (
	github.com/hashicorp/terraform v0.12.19
	github.com/juju/errors v0.0.0-20170509134257-8234c829496a
	github.com/juju/gomaasapi v0.0.0-20190826212825-0ab1eb636aba
	github.com/juju/loggo v0.0.0-20170605014607-8232ab8918d9
	github.com/juju/schema v0.0.0-20160916142850-e4e05803c9a1
	github.com/juju/utils v0.0.0-20180424094159-2000ea4ff043
	github.com/juju/version v0.0.0-20161031051906-1f41e27e54f2
	golang.org/x/crypto v0.0.0-20190701094942-4def268fd1a4
	gopkg.in/juju/names.v2 v2.0.0-20170515224847-0f8ae7499c60
	gopkg.in/mgo.v2 v2.0.0-20160818020120-3f83fa500528
	gopkg.in/yaml.v2 v2.2.2
)

replace github.com/juju/gomaasapi => ../gomaasapi
