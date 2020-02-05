module github.com/seanhoughton/terraform-provider-maas

go 1.13

require (
	github.com/hashicorp/terraform v0.12.19
	github.com/juju/clock v0.0.0-20190205081909-9c5c9712527c // indirect
	github.com/juju/collections v0.0.0-20180717171555-9be91dc79b7c // indirect
	github.com/juju/errors v0.0.0-20170509134257-8234c829496a // indirect
	github.com/juju/gomaasapi v0.0.0-20190826212825-0ab1eb636aba
	github.com/juju/retry v0.0.0-20180821225755-9058e192b216 // indirect
	github.com/juju/schema v0.0.0-20160916142850-e4e05803c9a1 // indirect
	github.com/juju/testing v0.0.0-20191001232224-ce9dec17d28b // indirect
	gopkg.in/juju/names.v2 v2.0.0-20170515224847-0f8ae7499c60 // indirect
	gopkg.in/mgo.v2 v2.0.0-20160818020120-3f83fa500528 // indirect
)

replace github.com/juju/gomaasapi => github.com/seanhoughton/gomaasapi v0.0.0-20200203223356-faf88ac35a2e
