go-getmail
==========
This Go program is a simple tool to retrieve and forward e-mails between IMAP servers.

[![Build Status](https://travis-ci.org/mback2k/go-getmail.svg?branch=master)](https://travis-ci.org/mback2k/go-getmail)
[![GoDoc](https://godoc.org/github.com/mback2k/go-getmail?status.svg)](https://godoc.org/github.com/mback2k/go-getmail)
[![Go Report Card](https://goreportcard.com/badge/github.com/mback2k/go-getmail)](https://goreportcard.com/report/github.com/mback2k/go-getmail)

Dependencies
------------
Special thanks to [@emersion](https://github.com/emersion) for creating and providing
the following Go libraries that are the main building blocks of this program:

- https://github.com/emersion/go-imap
- https://github.com/emersion/go-imap-idle

Additional dependencies are the following awesome Go libraries:

- https://github.com/spf13/viper

Installation
------------
You basically have two options to install this Go program package:

1. If you have Go installed and configured on your PATH, just do the following go get inside your GOPATH to get the latest version:

```
go get -u github.com/mback2k/go-getmail
```

2. If you do not have Go installed and just want to use a released binary,
then you can just go ahead and download a pre-compiled Linux amd64 binary from the [Github releases](https://github.com/mback2k/go-getmail/releases).

Finally put the go-getmail binary onto your PATH and make sure it is executable.

Configuration
-------------
The following YAML file is an example configuration with one transfer to be handled:

```
Accounts:

- Name: Test account
  Source:
    IMAP:
      Server: imap-source.example.com:993
      Username: your-imap-source-username
      Password: your-imap-source-username
      Mailbox: your-imap-source-mailbox
  Target:
    IMAP:
      Server: imap-target.example.com:993
      Username: your-imap-target-username
      Password: your-imap-target-username
      Mailbox: your-imap-target-mailbox
```

You can have multiple accounts handled by repeating the `- Name: ...` section.

Save this file in one of the following locations and run `./go-getmail`:

- /etc/go-getmail/go-getmail.yaml
- $HOME/.go-getmail.yaml
- $PWD/go-getmail.yaml

License
-------
Copyright (C) 2019  Marc Hoersken <info@marc-hoersken.de>

This software is licensed as described in the file LICENSE, which
you should have received as part of this software distribution.

All trademarks are the property of their respective owners.
