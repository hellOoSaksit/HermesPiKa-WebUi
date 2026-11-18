// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package assets embeds the files hpk ships inside its single binary:
// the HPK Console, wiki templates and the skills hpk installs into Hermes.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:console templates skills cron
var embedded embed.FS

func sub(dir string) fs.FS {
	s, err := fs.Sub(embedded, dir)
	if err != nil {
		panic(err) // embedded at build time; cannot fail at run time
	}
	return s
}

// Console is the HPK Console web app (plain HTML, CSS and JS, no build step).
func Console() fs.FS { return sub("console") }

// WikiTemplates holds SCHEMA.md, index.md and log.md for a new project wiki.
func WikiTemplates() fs.FS { return sub("templates/wiki") }

// Skills holds the SKILL.md folders hpk installs into a Hermes profile.
func Skills() fs.FS { return sub("skills") }

// CronPrompts holds the prompts of the scheduled jobs (nightly, weekly, digest).
func CronPrompts() fs.FS { return sub("cron") }
