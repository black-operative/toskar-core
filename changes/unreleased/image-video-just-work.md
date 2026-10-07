### Fixed

- Asking chat for a picture or a clip makes it. "Draw a dog", "please
  generate an image of a dog", or "are you able to make a picture of a dog
  for me?" used to get "I can't draw" or a list of stock-photo sites from
  a small model, though image generation was set up. Toskar now makes the
  picture itself, with the model only writing what to draw, and the same
  for changing an attached picture or making a short video.
- Pictures, clips, audio read aloud, files code saved, and page
  screenshots are attached to the answer that made them; only files
  created as documents were before.
- A request for a picture asked as a question, before image generation is
  set up, gets the setup offer with the request, so it's finished once
  the model is ready, instead of only "No, I can't generate images right
  now".
