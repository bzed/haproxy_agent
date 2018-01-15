# haproxy_agent

Agent for haproxy that exposes the current CPU status.

```
$ ./haproxy_agent --help
Usage of ./haproxy_agent:
  -drain-file string
    	Path to a file that if present should set the status to 'drain'
  -log-requests
    	log each request with remote IP and returned health
  -maint-file string
    	Path to a file that if present should set the status to 'maint'
  -port int
    	tcp port to use (ignored if activated via systemd)
  -systemd
    	Use systemd activation mechanism
  -timeframe int
    	calculate cpu usage for this timeframe in milliseconds (default 2000)
```

## Server mode

By default, the agent will do a single check against the CPU, report the status
over STDOUT and exit. If you want to have it running continously, you have two
options:

* You can explicitly define a TCP port the service should be listening on using
  the `-port` flag:

  ```
  $ ./haproxy_agent -port 7777
  2018/01/15 16:22:53 Listening on port 7777
  ```

* If you're using systemd, you can use its socket activation for launching the
  service on demand. As the setup for this is a bit more complicated, you can
  find details about it in the section "systemd socket support" down below.


## systemd socket support

systemd offers a mode similar to xinitd for launching services on demand
instead of at boot-time. You can use the `-systemd` flag to use this mode:

```
$ /lib/systemd/systemd-activate -l 127.0.0.1:7778 ./haproxy_agent -systemd &
[1] 25940
Listening on 127.0.0.1:7778 as 3.

$ nc 127.0.0.1 7778
Communication attempt on fd 3.
Execing ./haproxy_agent (./haproxy_agent -systemd)
2018/01/15 16:41:35 Using systemd activation
100% ready
```


## Draining and maintenance

If you want to indicate through this service if the node should be draint or
moved to marked as "in maintenance" within haproxy, you can specify two status
files that influence the status output:

```
$ ./haproxy_agent \
    -drain-file $PWD/drain \
    -maint-file $PWD/maint
98% ready

#
# If the drain-file exists:
#
$ touch drain
$ ./haproxy_agent \
    -drain-file $PWD/drain \
    -maint-file $PWD/maint
98% drain

#
# If the maint- and drain-file exist:
#
$ touch maint
$ ./haproxy_agent \
    -drain-file $PWD/drain \
    -maint-file $PWD/maint
98% maint

#
# If only the maint-file exists:
#
$ rm drain
$ ./haproxy_agent \
    -drain-file $PWD/drain \
    -maint-file $PWD/maint
98% maint
```
