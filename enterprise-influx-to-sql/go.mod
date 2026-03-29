module github.com/qube-enterprise/enterprise-influx-to-sql

go 1.22

require (
	github.com/influxdata/influxdb1-client v0.0.0-20220302092344-a9ab5670611c
	github.com/qube-enterprise/pkg v0.0.0
	gopkg.in/yaml.v2 v2.4.0
	modernc.org/sqlite v1.29.6
)

replace github.com/qube-enterprise/pkg => ../pkg
