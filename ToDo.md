# ToDos

### sandbox

- Add implementation to re-/build to Start Option (-> build-container.sh)

- test new tui interface
- test if run "setup...-skills" works if skills are in folder

- test create dist release script

- copy templates/docs on init to project folder
	
- integrate start better into OS
  - symlink with oc_sandbox -> on mac?
  - desktop entry -> on WSL / MAC?
  
- update build-container.sh to support building only singe container and editions
  
- as a developer, i want to add an webui mode --oc_webui flag: run opencode webui --port 4096 -> port passthroug im Startscript für OC Weboberfläche

### Docker image


### HIL Mode
  - change the picoscope usb mount mechanism from mounting whole bus to the same as usb µC devices is possible (but without symlinks)

### afkLoop
  - --verbose flag for output in terminal
  - add a short sleep time between iteration -> not accidentaly start when already doen
  - change LoopPrompt after two or three failing attempts to one with bug fix approach.
  - give the stdout to next loop?
  - give error-log to next loop (ater thee? failing attempts?)

# Bugs	


# in progress
    





# Done
- pulling skills from github on init into project skill folder (skills folder on each):
  1. https://github.com/mattpocock/skills
  2. https://github.com/shredingerlabs/shredinger-skills
  -> TEST
- add simavr to Dockerfile
- add glab to Dockerfile
- add glab .config the same was as gitlab 
- glab: failed to read configuration:  /home/dev/.git_local/glab-cli/config.yml has the permissions 664, but glab requires 600.
- write a HIL-Skill
  - Voltage level 5V
  - show planned hardware setup ( used USB ports, GPIO pins, probe connections, osci voltage level) before running tests and wait for user to confirm
  - do not run tests, scripts or software that needs different hardware setups (e.g different GPIO pins, probes connected differently, ...) without asking
  - ask if hardware setup is ready before running tests
- check if container exists and ask to start new one -> --restart flag used
- refractor oszi to osci
- add loop file
- on loop I'd go with (1) for the first cut. You can always upgrade to (2) if "blocked" tickets start burning retries.
- add picoscope.md in template scope for HIL testing
- Umstellen von slip4nets auf Pasta
- mit paste prüfen ob proxy noch tut (ja, nur umstellen im container)
- proxy config per projekt
- create install-script: ein curl befehl mit copy nach $HOME/.opencode_sandbox/ und app eintrag mit start in terminal
- add CBM commands to readme 
   codebase-memory-mcp / codebase-memory-mcp config set auto_index true
- update readme slirp4nents -> pasta
- add fetch gum binary to install script / include installGum script to installation process
- move gum install from start to install script
- test install script
- on start, check if container is running, then login, not restart
One common start and setup mechanism / script:
- on init
  - ask for project name and folder -> store
  - ask for git host and token -> copy only these files
  - ask for gdwg saia token -> copy opencode.json with gwdg models and create auth.json with token and host
  - ask for opencode go token -> add to auth.json
- on startup
  1. Start 
    - select project from list
    - select modes (proxy, offline, cbm-ui, hil, ...)
    - run opencode with setup-matt-pocock-skills und Ausgabe direkt im Terminal
    - run codebase-memory auto-index
  2. to n. Start: 
    - select project from list
    - select modes (proxy, offline, cbm-ui, hil, ...)
    - Start Opencode and codebase-memory
- install better-sqlite3
- implement wayfinder map TUI (untested)
