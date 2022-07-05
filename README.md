## Lightning Network Daemon - Bolt 12 Experimental Fork

[![Build Status](https://img.shields.io/travis/lightningnetwork/lnd.svg)](https://travis-ci.org/lightningnetwork/lnd)
[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/lightningnetwork/lnd/blob/master/LICENSE)
[![Irc](https://img.shields.io/badge/chat-on%20libera-brightgreen.svg)](https://web.libera.chat/#lnd)
[![Godoc](https://godoc.org/github.com/lightningnetwork/lnd?status.svg)](https://godoc.org/github.com/lightningnetwork/lnd)

### About
This is an experimental fork of LND which adds external support for [bolt 12](https://github.com/lightning/bolts/pull/798).

*Please do not run this in production, ffs.*

### Installation
This code is largely implemented in an external library, to keep a clean 
separation of experimental code. However, we put it all together in lnd behind
the `bolt12` build tag so that we can take advantage of lnd's exception build 
and test infrastructure. 

All functionality can be accessed using the `bolt12` build tag, eg:

`make install tags=bolt12`
