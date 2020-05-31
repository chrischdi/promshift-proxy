FROM scratch
ADD promshift-proxy /promshift-proxy
ENTRYPOINT ["/promshift-proxy"]
