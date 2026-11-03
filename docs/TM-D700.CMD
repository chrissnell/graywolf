;This is a sample TNC initialisation file for use with
;the Kenwood TM-D700.
;TM-D700.ORG will allow you to revert to the default
;version of this file, if you edit it and mess it up!

[SETUP]
;DON'T alter anything in this section unless you are
;sure you know what you are doing!
COMMAND_PROMPT=cmd:
COMMAND_CHARACTER_CODE=3
ESCAPE_CHARACTER_CODE=
CONV_COMMAND=CONV
MYCALL_COMMAND="MYCALL "
UNPROTO_COMMAND="UNPROTO "
NO_BEACON_COMMAND=BEACON EVERY 0

[INIT_COMMANDS]
^C
AI 1!AI 1
;Control mode on.
TC 1
;Select TNC PKT mode.
TNC 2!TNC 2
;Select band 'A'.
BC 0,0!BC 0,0
;Set the frequency to 144.800.
FQ 00144800000,0!FQ 00144800000,0
;No shift.
SFT 0!SFT 0
;Select data band as 'A'.
DTB 0!DTB 0
;Control mode off.
TC 0!cmd:!5
;Repeating the first two commands is not an error!
ECHO OFF
BEACON EVERY 0
ECHO OFF
BEACON EVERY 0
DIGI OFF
GBAUD 4800
HBAUD 1200
GPSTEXT $GPRMC
LTMON 10
LTMHEAD ON
LOC E 0
MON ON
MCOM ON
MSTAMP OFF
CONOK OFF
PACLEN 250
UIDIGI OFF
UIFLOOD %
UITRACE %
XFLOW OFF
HEADER OFF
NEWMODE ON
NOMODE OFF

[EXIT_COMMANDS]
LTMON 0
MON OFF
TC 1
TNC 0
