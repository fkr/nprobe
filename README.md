# nprobe

nprobe is a replacement for smokeping. Smokeping is a tool from the 90s, that to this day I 
still use. However while it still serves its purpose, it has not aged very well.
nprobe tries to address this.

## Warning

nprobe is currently being very actively worked on. The datasink backend will still change a lot,
so will the API and datastructured. Look at it, play with it, but **don't** use it to do anything
where you care for data. As soon as I've reached a point, where I will be sure about not breaking
the datalayer anymore, I'll remove this warning.

## Architecture

For the design, please check design.md in the documentation folder. The tl;dr is:

* there is a head node
* there are multiple satellites
* there are various targets defined on the head node that can be assigned to satellites

The satellites connect to the head node, receive the targets they're supposed to probe and send
back their results to the head. The head throws the received data into a datasink and that data
can be graphed.

## Setup

### Head Node

There is ``config/config.yaml.example`` which serves as a template. Copy this 
to ``config/config.json``, which is the default where the config is looked for.

Usage can be displayed with:

```
$ ./nprobe --help
```

On the head node it should be enough to just start nprobe with:

```
$ ./nprobe
```

### Satellite node

The satellite node needs to have its secret configured via an environment variable:

```
$ export NPROBE_SECRET=secret-defined-for-the-satellite-in-head-config
```

The nprobe satellite needs to be passed where to find the head node:

```
$ ./nprobe --head nprobe.example.com --name my-satellite-name
```

The name of the satellite is derived from the hostname, if it differs, it needs to be passed.


### Further CLI flags

```
$ ./nprobe --help
Usage of ./nprobe:
  -config string
    	config file (default "config/config.json")
  -debug
    	enable debug mode
  -head string
    	fqdn / ip of head node
  -insecure-tls
    	disable use of tls cert checking
  -mode string
    	head / satellite (default "satellite")
  -name string
    	name of probe (defaults to fqdn)
  -notls
    	disable use of tls
  -privileged
    	enable privileged mode
```

### Access to raw sockets

The underlying use of ICMP for the icmp probes requires certain socket semantics on the 
used operating system of the satellites. Depending on your os, you might need to run this with
elevated privileges (aka: run it as root with ```--privileged`` flag passed as well)


