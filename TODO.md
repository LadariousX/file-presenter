## next revision plan
usr launches app and picks folder, the browser opens and a qr is displayed. qr will take to the laydenb.com/file-presenter/remote. 
pass through bluetooth device name so the remote device and attempt to connect to the presenting device. the two devices will 
use bluetooth to communicate controls and thumbnails. please speak up if you have any concerns with this framework. im 
thinking about a js that runs in phone browser and relies on bluetooth to interface with the phone. the site is simply
a place where users can get the remote code. 

### initial changes
- remote control will work over Bluetooth sending cmd and receiving compressed (lesser quality) thumbnails
- drop chromium, launch the default browser and kill when main.go is terminated
- add a switch to skip duplicates (just by filename) so if already viewed will skip
- rm next unseen button
- only show the pause /play button when viewing videos. 
- display filenames as folder/subfolder/subsubdir and alternate the colors of the folder/ names then the filename between two grays
- rename list to files
- when usr presses the up button, move the list to the dir they just left and highlight it with the same green. just read the open filepath to highlight the next path if available. 
- to prevent a remote page from zooming or scrolling when on the primary page, ask clicking is recognized as a double tap zoom
- when the app launches, give the user a file picker so they can nav to the top directory
- add a button in the files menu to clear progress.
- if user chooses to nav to a dir, let them crawl fwd from that point.  
- I just want to make sure you're not keeping all of those thumbnails.
- primary page has a flag button (silhouette or filled) which moves that file to the favorites directory at the top lvl.

