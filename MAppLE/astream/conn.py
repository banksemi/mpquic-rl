from ctypes import *
import os.path

current_dir_path = os.path.dirname(os.path.realpath(__file__))
lib = cdll.LoadLibrary("/build/proxy_module.so")

class GoSlice(Structure):
    _fields_ = [("data", POINTER(c_void_p)), 
                ("len", c_longlong), ("cap", c_longlong)]

class GoString(Structure):
    _fields_ = [("p", c_char_p), ("n", c_longlong)]


lib.ClientSetup.argtypes = [c_bool,c_bool,c_bool,GoString,GoString]
lib.DownloadSegment.argtypes = [GoString, GoString, c_uint]
lib.CloseConnection.argtypes = []
lib.StartLogging.argtypes = [c_uint]
lib.StopLogging.argtypes = []
lib.FECSetup.argtypes = [c_bool,GoString]

import time
last_time = None

# values need to be valid for the program's lifetime
schedulerNameEncoded = None
congestionControlNameEncoded = None
configEncoded = None

def setupPM(useQUIC, useMP, keepAlive, schedulerName, congestionControlName='cubic'):
    global schedulerNameEncoded
    global congestionControlNameEncoded
    schedulerNameEncoded = schedulerName.encode('ascii')
    congestionControlNameEncoded = congestionControlName.encode('ascii')
    scheduler = GoString(schedulerNameEncoded, len(schedulerNameEncoded))
    cc = GoString(congestionControlNameEncoded, len(congestionControlNameEncoded))
    lib.ClientSetup(useQUIC, useMP, keepAlive, scheduler, cc)

def setupFEC(useFEC, config):
    global configEncoded
    configEncoded = config.encode('ascii')
    configGoStr = GoString(configEncoded, len(configEncoded))
    lib.FECSetup(useFEC, configGoStr)

def closeConnection():
    lib.CloseConnection()

def download_segment_PM(segment_url, filename="", buffer_current_size_i=567):
    segment = GoString(segment_url.encode('ascii'), len(segment_url))
    filename_encoded = GoString(filename.encode('ascii'), len(filename))
    return lib.DownloadSegment(segment, filename_encoded, buffer_current_size_i)

def startLogging(period):
    lib.StartLogging(period)

def stopLogging():
    lib.StopLogging()
