# gogitserver

> Minimal public git server

## Features

	- Minimal
	- Hosts multiple repositories. Index file with list of repos
	- Repository tree navigation
	- File serving of bare repo files
	- Archive download
	- Clone link ('dumb HTTP')
	- JSON config file...

What is needed?

	- ./static/styling.css
	- ./templates/tree.html
	- ./templates/index.html
	- ~/.config/gogitserver.conf

Run with `-d` flag to enable console debug logging, otherwise logging will be done to a file called `./logfile.log`.

example config file:

```
{
	"repos":[
		{
			"name":"GitServer",
			"path":"./gitserver.git",
			"description": "Minimal server for git repos. Only shows latest commit tree. Dumb http clone and archive download supported."
		}, 
		{
			"name":"ymake",
			"path":"./ymake.git",
			"description": "YAML style makefile alternative. [Alpha]<a href=\"test\">test</a>"
		}

	], 
	"port":5888
}
```

([YMakefile](https://github.com/Nananas/ymake) is used to make.)