# gogitserver

> Minimal public git server

## Features

	- Minimal
	- Hosts multiple repositories. Index file with list of repos
	- Repository tree navigation
	- File serving of bare repo files
	- Archive download
	- Clone link ('dumb HTTP')
	- YAML config file...

What is needed?

	- ./static/styling.css
	- ./templates/tree.html
	- ./templates/index.html
	- ~/.config/gogitserver.conf

Run with `-d` flag to enable console debug logging, otherwise logging will be done to a file called `./logfile.log`.

example config file:

```
header: |
    <script>
    	// include analytics script here
    </script>

port: 5888
repos: 
  - 
  description: "Minimal server for git repos. Only shows latest commit tree. Dumb http clone and archive download supported."
    name: GitServer
    path: ./gitserver.git
    footer: |
      <div class="repo" style="position:absolute; bottom:50px; left:0; right:0; text-align:center; width:50%; margin:auto">
      <a href="http://github.com/Nananas/gogitserver" style="">Github</a>
      </div
```

([YMakefile](https://github.com/Nananas/ymake) is used to make.)