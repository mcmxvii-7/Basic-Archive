# BAsic ARchive
## Cmd
Create archive:
```
bar archive.bar files...
```
List archive contents:
```
bar -l archive.bar
```
Extract files:
```
bar -x archive.bar
bar -n name -x archive.bar # Extract specific file
bar -o -x archive.bar      # Override existing files
```

## Format
```
All data is written in litte-endian byte order.

BAR file structure:
[Header]
[Data]
[Table]
[Footer]

Header:
  magic    3 bytes
  version  1 byte

Data:
Array of entry data.
  Entry data:
    File data for entry compressed with DEFLATE.

Table:
Array of entries compressed with DEFLATE.
  Entry:
    compressed size    8 bytes
    uncompressed size  8 bytes
    index              8 bytes  (points to the start of the file data)
    adler32            4 bytes  (checksum of compressed file data)
    unix permissions   2 bytes
    name length        2 bytes
    name               variable

Footer:
  index    8 bytes  (points to the start of the table)
  adler32  4 bytes  (checksum of compressed table)
  count    4 bytes  (number of entries in the table)
```
