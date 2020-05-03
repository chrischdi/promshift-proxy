# PromShift Proxy

This is a small proxy to be used as Grafana datasource to dynamically modify
queries to a Prometheus API.

It currently supports modifying the following URL-encoded parameters which are
encoded in the URL:
* `start`
* `end`
* `time`

For more information about the parameters have a look at the [Prometheus docs].

[Prometheus docs]: https://prometheus.io/docs/prometheus/latest/querying/api/

## Why

I was building a Grafana Dashboard having day-specific values.
The sum of a day was always shown with the timestamp of the next day which may 
be a bit confusing for end-users.
By adjusting the `start`, `end` or `time` parameters by defining different
datasources I was able to shift the queries to:
* Show not too much values in my table.
* Shift the timestamp by 1 second to show the last second of the day (`23:59:59`)
  instead of the next day.

I was not able to achieve the same by Grafana configuration and that's why I have
written this small proxy.

## Usage

To adjust the parameters configure PromShift Proxy as normal 
[Prometheus datasource] and configure your datasource at Custom Query Parameters.

[Prometheus datasource]: https://grafana.com/docs/grafana/latest/features/datasources/prometheus/

The following configuration would:
* add `23h` to the timestamp `start` (if the parameter exists in the request)
* subtract `1h` from the timestamp `end` (if the parameter exists in the request)
* subtract `1h` from the timestamp `time` (if the parameter exists in the request)

```
start=23h&end=-1h&time=-1h
```



