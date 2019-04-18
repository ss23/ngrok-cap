Ever heard of [ngrok](https://ngrok.com/)? Check out their website to learn what they're for, but the short version is developers often expose internal corporate assets through short-lived ngrok URLs. This project aims to perform scans across all ngrok URLs to capture a snapshot of what developers are exposing at any given time.

## Installing
Installing it with `go get github.com/ss23/ngrok-cap` might work, but keep in mind you'll also need to get screenshotting working at some point.

## To do
- [ ] Store results of scans
- [ ] Randomize the order that scans are done instead of starting at 0

## Feedback?
Let me know what you think on Twitter, I'm [@ss2342](https://twitter.com/ss2342) there.
