# WIP transmission-remote alternative

Install:
`go install github.com/frizinak/gotor/cmd/gotor@latest`

Features:
- list (`transmission-remote -l`) (with name/path/id/date/status filters)
- info (`transmission-remote -i`)
- add (`transmission-remote -a`) (with "category" support: `gotor add ISO 'magnet:...'`)
- remove (`tranmssion -r/-rad`) (with the same filters as `list` and confirmation)
