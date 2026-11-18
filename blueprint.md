# Constitution:
I want to create a new system to help me debuging remote machines that don't have AI locally. Basically I want to let any Claude code (or other similar tool) session to temporarily connect to any remote that has this system installed and read files and issue bash commands instead of telling the user what do do via copy paste. Basically close the air gap.



# Workflow:

## Initial Flow
The user will first login to a webpage with their google account. For the MVP this is enough as auth [R1]. There he will be able to create a key that will later be used by the



The user would install a local CLI tool (henseforth known as "cli-tool" in leu of a better name), possibly via curl as it is fashioanble this days. [P1] This tool shall be installed to a user space directory in the users home folder. [P2] what happens if the tool is installed with sudo is to be decided later.

[P3] After installation the user must issue the cli-tool login command that will log it in a rest-api server henseforth known as "rest-api". The exact auth mechanism is not important right now. [p11] for the mvp we shall assume a pre-shared knwon token. After loging in the user is not able to start  will start a session posibly by running the tool with no arguments. [P4] For the mvp we will not have resume capabilities. every time a new session shall start. [P5] when a new session starts a strong, sufficiently long easy to copy key will be generated and displayed into the screen. [P6] by pressing ctrl+c the session is terminataed. [p9] the cli-tool behind the scenes is connected to the rest-api. p10 it is the API that generates these unique codes with a post command.

[P7] The secret code is then shared to claude code or anytoher tool henseforth known as "ai-tool". The ai-tool knows what to do with this key via a skill and/or mvp. and is able to connect to the rest-api too. via the rest-api the ai-tool is able to relay commands to the cli-tool

TODO: white list of commands, and directories

Components:



Security concernts:
1. Obviously this is almost, if not exactly, a backdoor with remote execution build in.
2. The local tool shall run as a plain user (TBD if it could ask for sudo).


tests must be lean as few as possible while at the same time offer coveragek