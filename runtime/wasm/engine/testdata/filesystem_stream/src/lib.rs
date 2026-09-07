wit_bindgen::generate!({ path: "wit", world: "fixture", generate_all });
use wasi::filesystem::types::{DescriptorFlags, OpenFlags, PathFlags};
use wasi::io::streams::StreamError;
struct Fixture;
impl Guest for Fixture {
 fn run() -> Result<String,String> {
  let dirs=wasi::filesystem::preopens::get_directories();
  let (root,_) = dirs.first().ok_or("no mount")?;
  let file=root.open_at(PathFlags::SYMLINK_FOLLOW,"input",OpenFlags::empty(),DescriptorFlags::READ).map_err(|e|format!("open: {e:?}"))?;
  let stream=file.read_via_stream(3).map_err(|e|format!("stream: {e:?}"))?;
  drop(file); // Stream must retain the opened file independently.
  let mut count=0u64;let mut sum=0u64;
  loop {
   match stream.blocking_read(1<<20) {
    Ok(data)=>{ count+=data.len() as u64;sum+=data.iter().map(|x|*x as u64).sum::<u64>(); },
    Err(StreamError::Closed)=>break,
    Err(e)=>return Err(format!("read: {e:?}")),
   }
  }
  drop(stream);
  let out=root.open_at(PathFlags::empty(),"output",OpenFlags::CREATE|OpenFlags::EXCLUSIVE,DescriptorFlags::WRITE).map_err(|e|format!("create: {e:?}"))?;
  let stream=out.write_via_stream(0).map_err(|e|format!("output stream: {e:?}"))?;
  drop(out);
  let answer=format!("{count}:{sum}");
  stream.blocking_write_and_flush(answer.as_bytes()).map_err(|e|format!("write: {e:?}"))?;
  drop(stream);
  Ok(answer)
 }
}
export!(Fixture);
