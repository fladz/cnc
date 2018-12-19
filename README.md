# cnc
CNC (Client Network Checker) - get client side network stats, save the result in Google Drive &amp; Google BigQuery

# Motivation
Occasionally, Customer Support staff receives user compalins regarding network issue (slow connection to our services, etc) and it's not easy for Customer Support staff to explain what users need to check on their computer, so created a download-able tool that collectes data from clients and uploads it to our Google Team Drive (for other staff to see the stats - mainly Network staff) as well as in Google BigQuery (for easier reporting). 

# What is cnc
cnc is a downloadable tool for each Linux, Mac (supported versions: 10.11 ~ 10.14 64bits) and Windows (supported versions: 7, 8.1, 10 64bit) platform. Clients would download an executable, run it and that's all they need to do! After the data is received and saved, our internal staff would look at the stats and hopefully it's helpful enough for them to find out why the clients are experiencing (potential) network issues. 

cnc will collect the following information from client machine:
 - IP, ISP
 - OS (name, version, kernel, etc)
 - CPU
 - Memory (physical, virtual)

cnc will also run the following network test:
 - Ping (to specified destinations)
 - Traceroute (to specified destinations)
 - Download speed (from specified destinations)
 - More test coming soon!
 
Some logic are done in very brute-force-y way, if anyone has any suggestions, I'm happy to hear! :)


# What is cnc_listener
cnc_listener is an endpoint where cnc will send stats/test results to. cnc_listener is intended to work in GAE. 
cnc_listener will (a) create a Google Doc and upload it in a specified Google Drive and (b) insert received results in a specified BigQuery table.

You'll need to set up a GCP project and set up BigQuery table for this. 
Also make sure to set up a proper permissions for cnc_listener to be able to create files in a Google Drive and insert data in BigQuery.

This version doesn't have retries - if uploading to Google Drive fails or inserting to BigQuery fails, it doesn't retry. The data will stay there if inserting to BigQuery failed but uploading a Google Doc to Google Drive was successful (and vice-versa). 
