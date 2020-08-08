#!/usr/bin/env python3
import re
import random
import json
import pytz
import wave
import contextlib

import speech_recognition as sr
from os import path, environ, remove
from flask import Flask, request, jsonify, make_response
from flask_wtf.file import FileField
from wtforms import Form, StringField
from datetime import datetime
from pytz import timezone
from threading import BoundedSemaphore, Thread


GOOGLE_CLOUD_SPEECH_CREDENTIALS = environ["GOOGLE_CLOUD_SPEECH_CREDENTIALS"]
AUDIO_DUMP_PATH = environ["AUDIO_DUMP_PATH"]
DATA_FILE_PATH = environ["DATA_FILE_PATH"] 
WORD_DICT_PATH = environ["WORD_DICT_PATH"]

app = Flask(__name__)
total_play_time = None
quota_day = None
play_time_sem = BoundedSemaphore(1)

me_words = []

class ProcessAudioFile(Form):
	file = FileField(
		'file',
		[]
	)

@app.route('/api/process_audio_file', methods=["POST"])
def get_poop_photo_endpoint():
	fileForm = ProcessAudioFile(request.files)

	if not fileForm.validate() or fileForm.file.data == None:
		return make_response(
			jsonify({
				"result" : "error",
				"message" : "Invalid file"
			}),
			400
		)

	result = check_audio_file(fileForm.file)

	return make_response(
		jsonify({
			"result" : "success",
			"say_muh" : result
		}),
		200
	)

def load_words():
	with open(WORD_DICT_PATH) as word_file:
		words = set(word_file.read().split())

	for word in words:
		if re.search("me$", word):
			me_words.append(word)

def get_PST_time():
	date_format='%m/%d/%Y %H:%M:%S %Z'
	date = datetime.now(tz=pytz.utc)
	date = date.astimezone(timezone('US/Pacific'))
	return date


def save_play_time():
	play_time_sem.acquire()
	try:
		data = {"play_time": total_play_time, "date": quota_day.strftime("%d/%m/%Y")}
		with open(DATA_FILE_PATH, "w") as write_file:
			json.dump(data, write_file)
	finally:
		play_time_sem.release()

def load_play_time():
	global total_play_time
	global quota_day
	with open(DATA_FILE_PATH, "r") as read_file:
		data = json.load(read_file)

	total_play_time = data["play_time"]
	quota_day = data["date"]
	if quota_day == "":
		quota_day = get_PST_time()
	else:
		quota_day = datetime.strptime(quota_day, '%d/%m/%Y')

def add_play_time(filepath):
	global total_play_time
	global quota_day

	duration = 15
	try:
		with contextlib.closing(wave.open(filepath,'r')) as f:
			frames = f.getnframes()
			rate = f.getframerate()
			duration = max(duration, frames / float(rate))
	finally:
		remove(filepath)

	play_time_sem.acquire()
	try:
		pst_time = get_PST_time()
		if quota_day.month != pst_time.month:
			print("New day in PST time")
			total_play_time = duration
			quota_day = pst_time
		else:
			total_play_time += duration
	finally:
		print("play time:{}".format(total_play_time))
		play_time_sem.release()

	save_play_time()

def quota_reached():
	return total_play_time > 3570

def init():
	load_words()
	load_play_time()

def say_muh_google(out):
	if len(out) == 0:
		return None

	for result in out["results"]:
		for alt in result["alternatives"]:
			if alt["confidence"] < 0.70:
				continue
			for word in alt["words"]:
				if word["word"] in me_words:
					return word

	return None

def get_audio_length(out):
	for result in out["results"]:
		for alt in result["alternatives"]:
			words = alt["words"]
			return float(words[-1]["endTime"][:-1])

	return 0

def google_handler(r, audio, filepath):
	muh_word = None
	try:
		out = r.recognize_google_cloud(
			audio,
			credentials_json=GOOGLE_CLOUD_SPEECH_CREDENTIALS,
			language="en-AU",
			# preferred_phrases="me",
			show_all=True
		)
		Thread(target=add_play_time, args=(filepath,)).start()

		muh_word = say_muh_google(out)
	except sr.UnknownValueError:
		print("Google Cloud Speech could not understand audio")
	except sr.RequestError as e:
		print("Could not request results from Google Cloud Speech service; {0}".format(e))

	return muh_word

def local_handler(r, audio, filepath):
	muh_word = None
	# recognize speech using Sphinx
	try:
		out = r.recognize_sphinx(
			audio,
			language="en-US",
				# keyword_entries=[("I am me", 0), ("the game", 0)]
		)
	
		for word in out.split():
			if word in me_words:
				muh_word = word

	except sr.UnknownValueError:
		print("Sphinx could not understand audio")
	except sr.RequestError as e:
		print("Sphinx error; {0}".format(e))

	return muh_word

def check_audio_file(file):
	handler = google_handler
	if quota_reached():
		handler = local_handler

	filepath = path.join(AUDIO_DUMP_PATH, "{}.wav".format(random.random()))
	file.data.save(filepath)

	r = sr.Recognizer()
	with sr.AudioFile(filepath) as source:
		audio = r.record(source)  # read the entire audio file
	muh_word = handler(r, audio, filepath)
	
	if muh_word == None:
		print("Word {}".format(muh_word))
		muh_word = False
	else:
		print("muh:{}".format(muh_word))
		muh_word = True

	return muh_word

init()

if __name__ == '__main__':
	app.run(host='0.0.0.0', port="5000")
