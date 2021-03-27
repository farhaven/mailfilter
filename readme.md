# Mailfilter
![Build](https://github.com/farhaven/mailfilter/workflows/Build/badge.svg)
![Lint](https://github.com/farhaven/mailfilter/workflows/Lint/badge.svg)

This is a very simple Bayesian classifier for RFC2046-formatted mail. It runs as an HTTP server and uses POST requests as its interface.

It uses a counting bloom filter to store ngram frequencies, which means that the amount of storage that it needs is bounded (currently to 128MB, 64MB each for the total counts and spam counts), at the cost of only storing approximations of the counts.

The way the bloom filter is built means that the frequencies will never be under-estimated, but with increasing diversity (which increases the probability of hash collisions), it will very likely get over-estimated. Since this affects both the spam count and the total count, the effects don't quite cancel each other out but are manageable.

The filter segments each text into ngrams of 4 bytes by using a sliding window across the text. This is done to mitigate the negative impact of padding or intentional typos on detection.

Here's how to use it:

## General usage

```
; ./mailfilter -help
Usage of ./mailfilter:
  -dbPath string
    	path to word database (default "${HOME}/.mailfilter.db")
  -listenAddr string
    	Listening address for profiling server (default "127.0.0.1:7999")
  -thresholdSpam float
    	Mail with score above this value will be classified as 'spam' (default 0.7)
  -thresholdUnsure float
    	Mail with score above this value will be classified as 'unsure' (default 0.3)
```

Start the server with `./mailfilter`. It'll run in the foreground and
serve requests on `127.0.0.1:7999`.

### Systemd service
You can also use a systemd service file that looks like this:

```
[Unit]
Description=Mail filtering daemon

[Service]
Type=simple
ExecStart=/path/to/mailfilter

[Install]
WantedBy=default.target
```

Place it into `~/.config/systemd/user/mailfilter.service` and run the
following commands to enable it and start it automatically when you
log in:

```
systemctl --user enable mailfilter
systemctl --user start mailfilter
```

To watch its output, you can use `journalctl --user -f -u mailfilter`.

## Train text as ham or spam

```
; cat /tmp/spam/*.msg | curl -f -XPOST --data-binary @- http://localhost:7999/train?as=spam
; cat /tmp/ham/*.msg | curl -f -XPOST --data-binary @- http://localhost:7999/train?as=ham
```

## Classify a message

```
; cat /tmp/new/bla.msg | curl -f -XPOST --data-binary @- http://localhost:7999/classify
```

This will write the content of `/tmp/new/bla.msg` to the standard
output stream. Immediately before the first empty line, a header with
the verdict will be inserted. It looks like this:

```
X-Mailfilter: label="spam", score=1.0000
```

for a message that the filter is very sure is spam. Available labels are:

* `spam` for everything with a score above 0.7
* `unsure` for everything with a score between 0.3 and 0.7
* `ham` for everything else

The thresholds can be changed by passing appropriate command line parameters.

## Maildrop
If you use maildrop, you can hook up mailfilter by adding a line like this to `~/.mailfilter`:

```
xfilter "curl -f -XPOST --data-binary @- http://localhost:7999/classify"
```

After that, you can use header matches for `X-Mailfilter: label="spam"`
and `X-Mailfilter: label="unsure"` to sort away mail:

```
if (/^X-Mailfilter: label="spam"/:h)
	TAGS="$TAGS +spam"
if (/^X-Mailfilter: label="unsure"/:h)
	TAGS="$TAGS +unsure"
```

## (Neo)mutt
If you use (neo)mutt to read your mail, you can add the following key
bindings to train mail as spam or ham:

```
macro index,pager S "<pipe-entry>curl -f -XPOST --data-binary @- http://localhost:7999/train?as=spam" "mark message as Spam"
macro index,pager H "<pipe-entry>curl -f -XPOST --data-binary @- http://localhost:7999/train?as=ham" "mark message as Ham"
```

This will make `S` and `H` open a prepared commandline inside mutt to
pass the mail the cursor is on (or the mail you're currently viewing)
to mailfilter if you press return.

## Things it does well (in my opinion)
Training is reasonably fast. Training my personal archive of OpenBSD's
Misc mailing list (roughly 70k messages) as ham takes about 2.5
minutes.

The code is quite small, and it fits quite nicely in a classical pipeline
of delivery agents and mail filters.

## Things is doesn't do well (in my opinion)
This filter is very very simple and was hacked together as a "I need to
sit on my couch and relax"-type project. The following caveats apply:

* Base64 content is not decoded. If you use `maildrop`, it'll do the decoding before filtering the message though.
* There is no garbage collection on the training data
* There are only three labels: "spam", "unsure" and "ham"

Each of those may change, or it may stay that way forever.