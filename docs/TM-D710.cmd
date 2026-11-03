;This is a sample TNC initialisation file for use with
;the Kenwood TM-D710 & UIView32.
;TM-D710.ORG will allow you to revert to the default
;version of this file, if you edit it and mess it up!
;


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
^C^C^C
;control mode on
TC 1!TS 1
;select TNC PKT mode on A band
TN 2,0!TN 2,0
;waiting for command prompt
^M!cmd:!5
;DIGIPEAT or DIG
;values OFF/ON
;default ON
DIG OFF
;GBAUD or GB baud rate of the GPS hooked up to the radio,
;values 2400/4800/9600
;default 4800
GB 4800
;HBAUD or HB TNC baud rate
;values 1200/9600
;default 1200
HB 1200
;GPSTEXT or GPST
;values $GPRMC/$GPGGA/$PNTS
;others may work haven't tried them
;default $PNTS
GPST $GPRMC
;LTMMON or LTM
;Specifies the interval for displaying a message
;determined by LTEXT on the screen 1 second intervals
;values 0-250
;default is 0 or off.
LTM 10
;LTMHEAD or LTMH
;values OFF/ON
;default is ON
LTMH ON
;LOCATION or LOC method of sending GPS Data
;10 second intervals
;parameters EVERY or E, AFTER or A
;values 0-250
;default 0 or off
LOC E 0
;MONITOR or M monitor packets
;values OFF/ON
;default ON
M ON
;MCOM moniter control packets
;values OFF/ON
;default OFF
MCOM ON
;MSTAMP or MS display data and time info for packets
;values ON/OFF
;default OFF
MS OFF
;CONOK or CONO accept request and return UA packet
;values ON/OFF
;default ON
CONO OFF
;PACLEN or P max length of data
;values 0-255
;default 128
P 250
;UIDIGI or UI
;values OFF/ON CALL1,...,CALL14
;default OFF
UI OFF
;UIFLOOD OR UIF
;values ID/NOID/FIRST
;default NOID
UIF NOID
;UITRACE or UIT
;values 5 alphanumeric characters
;default
UIT %
;XFLOW or X software flow contorl
;values OFF/ON
;default ON
X OFF
;HEADERLN or HE
;values OFF/ON
;default OFF
HEADER OFF
;NEWMODE or NE
;values OFF/ON
;default OFF
NE ON
;NOMODE or NO
;values OFF/ON
;default OFF
NO OFF

;repeating the first two commands is not an error!
ECHO OFF
BEACON EVERY 0
ECHO OFF
BEACON EVERY 0

[EXIT_COMMANDS]
LTM 0