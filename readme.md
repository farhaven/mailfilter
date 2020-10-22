# Mailfilter

This is a very simple Bayesian classifier for RFC2046-formatted mail.

Here's how to use it:

## General usage

```
; ./mailfilter -help
Usage of ./mailfilter:
  -dbPath string
    	path to word database (default "/home/gbe/.mailfilter.db")
  -dump
    	dump frequency data to stdout
  -mode string
    	What do do with the message. One of [classify, spam, ham]. (default "classify")
  -thresholdSpam float
    	Mail with score above this value will be classified as 'spam' (default 0.7)
  -thresholdUnsure float
    	Mail with score above this value will be classified as 'unsure' (default 0.3)
  -verbose
    	be more verbose during training
```

## Train text as ham or spam

```
; cat /tmp/spam/*.msg | ./mailfilter -mode=spam
; cat /tmp/ham/*.msg | ./mailfilter -mode=ham
```

## Classify a message

```
; cat /tmp/new/bla.msg | ./mailfilter -mode=classify
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
xfilter "/path/to/mailfilter -mode=classify"
```

After that, you can use header matches for `X-Mailfilter: label="spam"`
and `X-Mailfilter: label="unsure"` to sort away mail:

```
if (/^X-Mailfilter: label="spam"/:h)
	TAGS="$TAGS +spam"
if (/^X-Mailfilter: label="unsure"/:h)
	TAGS="$TAGS +unsure"
```

## Things it does well (in my opinion)
Training is reasonably fast. Training my personal archive of OpenBSD's
Misc mailing list (roughly 70k messages) as ham takes about 30 seconds.

The code is quite small, and it fits quite nicely in a classical pipeline
of delivery agents and mail filters

## Things is doesn't do well (in my opinion)
This filter is very very simple and was hacked together as a "I need to
sit on my couch and relax"-type project. The following caveats apply:

* Base64 content is not decoded
* There is no garbage collection on the training data
* There are only three labels: "spam", "unsure" and "ham"

Each of those may change, or it may stay that way forever.