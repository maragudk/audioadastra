// Upload forms, marked with data-upload, are sent with XMLHttpRequest rather than fetch, since only it
// reports upload progress. Progress and outcome are dispatched as events on the form, for Datastar to
// render: upload-progress with {percent}, upload-publishing once the file is sent, and upload-failed
// with {error}. On success the server answers with where to go next.

// A file over the form's data-max-size is refused before anything is sent, with its data-too-big message.
document.addEventListener("change", (event) => {
  const input = event.target;
  if (!(input instanceof HTMLInputElement) || input.type !== "file" || !input.form?.matches("form[data-upload]")) {
    return;
  }
  const file = input.files[0];
  const tooBig = file && file.size > Number(input.form.dataset.maxSize);
  input.setCustomValidity(tooBig ? input.form.dataset.tooBig : "");
  input.reportValidity();
});

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement) || !form.matches("form[data-upload]")) {
    return;
  }
  event.preventDefault();

  const send = (name, detail) => form.dispatchEvent(new CustomEvent(name, { detail }));
  const fail = (error) => send("upload-failed", { error });

  const xhr = new XMLHttpRequest();
  xhr.open("POST", form.action);
  xhr.setRequestHeader("Accept", "application/json");
  xhr.responseType = "json";
  xhr.upload.addEventListener("progress", (e) => {
    if (e.lengthComputable) {
      send("upload-progress", { percent: Math.floor((e.loaded / e.total) * 100) });
    }
  });
  xhr.upload.addEventListener("load", () => send("upload-publishing"));
  xhr.addEventListener("load", () => {
    if (xhr.status === 200 && xhr.response?.redirect) {
      window.location.assign(xhr.response.redirect);
      return;
    }
    fail(xhr.response?.error || "Something went wrong. Try again in a moment.");
  });
  xhr.addEventListener("error", () => fail("The upload was interrupted. Check your connection and try again."));

  send("upload-progress", { percent: 0 });
  xhr.send(new FormData(form));
});
